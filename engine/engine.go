// Package engine is the turn pipeline: decide, collect context, think (with
// tools), clean, deliver, remember. It owns no platform and no provider; both
// arrive through interfaces.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/tools"
)

type personaLibrary = persona.Library

type Completer interface {
	Complete(ctx context.Context, r provider.Request) (provider.Response, error)
}

type Engine struct {
	Cfg   *config.Store
	LLM   Completer
	Mem   *memory.Store
	Lib   persona.Library
	Tr    sdk.Transport
	Tools *tools.Registry
	Tel   *telemetry.Log
	Ev    *events.Hub
	Pol   *Policy
	Inc   *Incidents
	Log   *slog.Logger

	mood  *persona.Mood
	hooks []sdk.Hooks

	mu     sync.Mutex
	seen   map[string]time.Time
	locks  map[string]*sync.Mutex
	seq    map[string]uint64
	recent map[string][]string // her last public replies per channel
	// Sleep is a test seam; production uses time.Sleep honoring ctx.
	Sleep func(ctx context.Context, d time.Duration)
}

func New(cfg *config.Store, llm Completer, mem *memory.Store, lib persona.Library, tr sdk.Transport) *Engine {
	e := &Engine{
		Cfg: cfg, LLM: llm, Mem: mem, Lib: lib, Tr: tr,
		Tools: tools.NewRegistry(), Tel: telemetry.New(500, ""), Ev: events.NewHub(300), Pol: NewPolicy(), Inc: NewIncidents(),
		Log:  slog.Default(),
		mood: persona.NewMood(), seen: map[string]time.Time{}, locks: map[string]*sync.Mutex{},
		seq: map[string]uint64{}, recent: map[string][]string{},
		Sleep: func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
			case <-ctx.Done():
			}
		},
	}
	e.Tools.Disabled = func() []string { return e.Cfg.Get().Tools.Disabled }
	for _, t := range tools.Builtins(tools.Deps{
		Mem:        mem,
		Persona:    func() string { return e.Cfg.Get().Persona.Active },
		SearxURL:   func() string { return e.Cfg.Get().Search.SearxngURL },
		SelfReview: e.selfReview,
	}) {
		e.Tools.Add(t)
	}
	e.Tools.Add(tools.ReactionGIF())
	e.Tools.Add(tools.ReadSkill(func() home.Home { return e.Home() }))
	e.Tools.Add(tools.WriteFile(func() string { return filepath.Join(filepath.Dir(cfg.Abs(cfg.Get().Memory.Path)), "workspace") }))
	for _, t := range tools.Anime() {
		e.Tools.Add(t)
	}
	return e
}

// Use registers a plugin's tools and hooks.
func (e *Engine) Use(p sdk.Plugin) {
	for _, t := range p.Tools() {
		e.Tools.Add(t)
	}
	e.mu.Lock()
	e.hooks = append(e.hooks, p.Hooks())
	e.mu.Unlock()
}

// Home is the .makizu directory: the folder holding the config file.
func (e *Engine) Home() home.Home { return home.Home{Dir: filepath.Dir(e.Cfg.Path())} }

func (e *Engine) personaCfg() (persona.Persona, error) {
	c := e.Cfg.Get()
	lib := e.Lib
	if lib.Dir == "" {
		lib.Dir = e.Cfg.Abs(c.Persona.Dir)
	}
	return lib.Load(c.Persona.Active)
}

// Run blocks, serving the transport until ctx ends.
func (e *Engine) Run(ctx context.Context) error {
	if ch, ok := e.Tr.(sdk.CommandHost); ok && e.Cfg.Get().Discord.RegisterCommands {
		if err := ch.RegisterCommands(ctx, e.Commands()); err != nil {
			e.Log.Warn("commands", "err", err)
		}
	}
	go e.reminderLoop(ctx)
	go e.maintenanceLoop(ctx)
	go func() { e.Sleep(ctx, 10*time.Second); e.Catchup(ctx) }()
	return e.Tr.Run(ctx, func(ctx context.Context, m sdk.Message) { go e.Handle(ctx, m) })
}

func (e *Engine) lock(ch string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l := e.locks[ch]
	if l == nil {
		l = &sync.Mutex{}
		e.locks[ch] = l
	}
	return l
}

// Handle processes one inbound message end to end.
func (e *Engine) Handle(ctx context.Context, m sdk.Message) {
	self := e.Tr.Self()
	if m.AuthorID == self.ID || m.Content == "" && len(m.Attachments) == 0 {
		return
	}
	e.mu.Lock()
	if _, dup := e.seen[m.ID]; dup && m.ID != "" {
		e.mu.Unlock()
		return
	}
	e.seen[m.ID] = time.Now()
	for k, t := range e.seen { // bounded dedupe window
		if time.Since(t) > 10*time.Minute {
			delete(e.seen, k)
		}
	}
	e.mu.Unlock()

	cfg := e.Cfg.Get()
	pa, err := e.personaCfg()
	if err != nil {
		e.Log.Error("persona", "err", err)
		return
	}
	if pa.Mood {
		e.mood.Observe(m.AuthorName, m.Content, m.Mentioned || m.IsDM)
	}
	ok, reason := e.Pol.Decide(m, cfg, pa.Name)
	e.heard(m, cfg, ok, reason)
	if !ok {
		return
	}

	// Burst collapse: when several ambient messages land together, only the
	// latest gets a turn. Direct calls always get theirs.
	e.mu.Lock()
	e.seq[m.ChannelID]++
	mine := e.seq[m.ChannelID]
	e.mu.Unlock()
	if s := cfg.Behavior.Response.BurstSettleSecs; s > 0 && !m.IsDM {
		e.Sleep(ctx, time.Duration(s*float64(time.Second)))
		e.mu.Lock()
		stale := e.seq[m.ChannelID] != mine
		e.mu.Unlock()
		if stale && !reason.Direct() {
			e.Tel.Add(telemetry.Record{Kind: "skipped", Channel: m.ChannelID, Cause: "coalesced"})
			return
		}
	}

	l := e.lock(m.ChannelID)
	l.Lock()
	defer l.Unlock()

	attempts := 2
	if !cfg.Behavior.Retry || !reason.Direct() {
		attempts = 1
	}
	for a := 1; a <= attempts; a++ {
		out := e.turn(ctx, m, reason, pa)
		if out.err == nil && out.verdict == guard.OK {
			return
		}
		cause := Classify(out.err, out.verdict)
		e.Inc.Record(m.ChannelID, cause, "")
		e.Tel.Add(telemetry.Record{Kind: "incident", Persona: pa.ID, Channel: m.ChannelID, Cause: string(cause), Detail: errString(out.err)})
		e.Ev.Emit(events.Event{Type: "incident", Place: placeOf(m), Reason: string(cause), Why: cause.Human(), Text: errString(out.err)})
		// Retry once, silently, but only if nothing with side effects ran and
		// the conversation has not moved on.
		moved := func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.seq[m.ChannelID] != mine }()
		fresh := m.Time.IsZero() || time.Since(m.Time) < 90*time.Second
		if a < attempts && cause.Transient() && !out.sideEffects && !moved && fresh {
			e.Sleep(ctx, 2*time.Second)
			continue
		}
		_ = e.Tr.Send(ctx, m.ChannelID, sdk.Reply{Text: FailureReply(cause, m.Content), ReplyToID: m.ID})
		return
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

type turnOut struct {
	err         error
	verdict     guard.Verdict
	sideEffects bool
}

// heavyRe unlocks the heavy tools (research, files, anime lookups). Casual
// chatter never matches, so it never pays for or triggers them.
var heavyRe = regexp.MustCompile(`(?i)(https?://|\b(search|look up|find out|latest|news|investiga|busca|averigua|resume this|summari[sz]e|anilist|myanimelist|airing|schedule|what'?s on|how many episodes|\bairs?\b|episodios de|score of|rating of|release date)\b|\b(make|write|create|build|generate|draft|hazme|armame|armá|escribime|escribe|creá|crea)\b.{0,40}\b(html|css|file|script|page|archivo|json|csv|markdown|svg|py|go)\b)`)

func (e *Engine) turn(ctx context.Context, m sdk.Message, reason Reason, pa persona.Persona) (out turnOut) {
	cfg := e.Cfg.Get()
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	e.Tr.Typing(ctx, m.ChannelID)

	// Memory is scoped to the speaker. Never mix people.
	_ = e.Mem.Touch(ctx, pa.ID, m.AuthorID, m.AuthorName)
	rel, _ := e.Mem.Relationship(ctx, pa.ID, m.AuthorID)
	mems, _ := e.Mem.Recall(ctx, pa.ID, m.AuthorID, m.Content, cfg.Memory.RecallLimit)
	var memText []string
	for _, x := range mems {
		memText = append(memText, x.Content)
	}

	pctx := persona.Context{
		Now: time.Now().Format("Monday 2 January 2006, 15:04 MST"), Platform: e.Tr.Name(),
		Speaker: m.AuthorName, Relationship: rel.Describe(), Memories: memText,
		LanguageHint: languageHint(cfg.Language),
	}
	if pa.Mood {
		pctx.Mood = e.mood.Describe()
	}
	if m.IsDM {
		pctx.Place = "a private DM with " + m.AuthorName
	} else {
		pctx.Place = strings.TrimSpace("#" + m.ChannelName + " in " + m.GuildName)
	}
	if ep, ok := e.Tr.(sdk.EmojiProvider); ok {
		pctx.Emojis = ep.EmojiNames(m.GuildID)
	}
	pctx.Rules = e.Home().Directives(m.GuildID, m.ChannelID)
	for _, sk := range e.Home().SkillList() {
		line := sk.Name
		if sk.Description != "" {
			line += ": " + sk.Description
		}
		pctx.Skills = append(pctx.Skills, line)
	}
	if b := e.Inc.Block(m.ChannelID); b != "" {
		pctx.Extra = append(pctx.Extra, b)
	}
	e.mu.Lock()
	hooks := append([]sdk.Hooks(nil), e.hooks...)
	e.mu.Unlock()
	for _, h := range hooks {
		if h.BeforeReply != nil {
			if x := h.BeforeReply(ctx, m); x != "" {
				pctx.Extra = append(pctx.Extra, x)
			}
		}
	}
	system := persona.Compose(pa, pctx)

	msgs := e.history(ctx, m, cfg.Behavior.Turn.HistoryLimit)
	cur := provider.Message{Role: provider.User, Content: label(m.AuthorName, m.Content)}
	for _, a := range m.Attachments {
		if strings.HasPrefix(a.ContentType, "image/") {
			cur.Images = append(cur.Images, a.URL)
		} else if a.Name != "" {
			cur.Content += fmt.Sprintf(" [attachment: %s]", a.Name)
		}
	}
	msgs = append(msgs, cur)

	heavy := heavyRe.MatchString(m.Content)
	maxTok := cfg.Behavior.Turn.MaxTokens
	if heavy {
		maxTok = cfg.Behavior.Turn.HeavyTokens
	}
	var queued []sdk.File
	var reactions []string
	var links []string
	env := &sdk.CallEnv{
		Speaker: sdk.Identity{ID: m.AuthorID, Name: m.AuthorName}, ChannelID: m.ChannelID, GuildID: m.GuildID, IsDM: m.IsDM, Persona: pa.ID,
		QueueFile: func(f sdk.File) { queued = append(queued, f) },
		React:     func(x string) { reactions = append(reactions, x) },
		AttachLink: func(u string) {
			if len(links) < 1 {
				links = append(links, u)
			}
		},
	}

	var specs []provider.ToolDef
	for _, s := range e.Tools.Specs(heavy) {
		specs = append(specs, provider.ToolDef{Name: s.Name, Description: s.Description, Schema: s.Schema})
	}
	var toolsUsed []string
	req := provider.Request{System: system, Messages: msgs, MaxTokens: maxTok}
	if pa.Temperature > 0 {
		t := pa.Temperature
		req.Temperature = &t
	}

	var resp provider.Response
	var text string
	rounds := 0
	maxRounds := max(cfg.Behavior.Turn.MaxRounds, 1)
	for rounds < maxRounds {
		req.Tools = specs
		resp, out.err = e.LLM.Complete(ctx, req)
		rounds++
		if out.err != nil {
			return out
		}
		if len(resp.ToolCalls) == 0 {
			text = resp.Text
			break
		}
		req.Messages = append(req.Messages, provider.Message{Role: provider.Assistant, Content: resp.Text, ToolCalls: resp.ToolCalls})
		for _, c := range resp.ToolCalls {
			out.sideEffects = true
			toolsUsed = append(toolsUsed, c.Name)
			req.Messages = append(req.Messages, provider.Message{Role: provider.ToolRole, ToolCallID: c.ID, Content: e.Tools.Call(ctx, c.Name, c.Args, env)})
		}
	}
	// A turn that ends on tool calls still owes the room a spoken answer.
	if text == "" && len(toolsUsed) > 0 {
		req.Tools = nil
		req.Messages = append(req.Messages, provider.Message{Role: provider.User, Content: "[system: tool results are in. Answer the person now, in your own voice, in chat style.]"})
		resp, out.err = e.LLM.Complete(ctx, req)
		rounds++
		if out.err != nil {
			return out
		}
		text = resp.Text
	}

	// Output pipeline. One regeneration is allowed for the whole turn.
	regen := 1
	final := ""
	for {
		cleaned, v := guard.Clean(text)
		if v != guard.OK {
			if regen > 0 && !out.sideEffects {
				regen--
				text, out.err = e.regenerate(ctx, req, "[system: your last draft was unusable. Answer the person again, in chat style.]")
				if out.err != nil {
					return out
				}
				continue
			}
			out.verdict = v
			return out
		}
		e.mu.Lock()
		recent := append([]string(nil), e.recent[m.ChannelID]...)
		e.mu.Unlock()
		if hits := guard.RoboticHits(cleaned); len(hits) > 0 && regen > 0 && !out.sideEffects {
			regen--
			text, out.err = e.regenerate(ctx, req, "[system: that draft sounded like customer support. Rewrite it as yourself, like a person typing in chat.]")
			if out.err != nil {
				return out
			}
			continue
		}
		if guard.IsLoop(cleaned, recent) && regen > 0 && !out.sideEffects {
			regen--
			text, out.err = e.regenerate(ctx, req, "[system: that is almost word for word something you already said here. Say something new, shorter, or just react.]")
			if out.err != nil {
				return out
			}
			continue
		}
		if guard.RepeatsActionOpening(cleaned, recent, 2) {
			cleaned = guard.StripLeadingAction(cleaned)
		}
		cleaned = guard.LimitEmojis(cleaned, cfg.Behavior.Turn.EmojiBudget)
		for _, h := range hooks {
			if h.FilterOutput != nil {
				cleaned = h.FilterOutput(cleaned)
			}
		}
		final = strings.TrimSpace(cleaned)
		break
	}
	if final == "" && len(reactions) == 0 && len(queued) == 0 && len(links) == 0 {
		out.verdict = guard.Empty
		return out
	}

	e.Inc.Resolve(m.ChannelID) // she answered; earlier failures are history
	e.audit(m, final, toolsUsed, len(queued))
	e.deliver(ctx, m, reason, pa, final, queued, reactions)
	for _, l := range links {
		_ = e.Tr.Send(ctx, m.ChannelID, sdk.Reply{Text: l})
	}

	e.mu.Lock()
	e.recent[m.ChannelID] = append(e.recent[m.ChannelID], final)
	if n := len(e.recent[m.ChannelID]); n > 8 {
		e.recent[m.ChannelID] = e.recent[m.ChannelID][n-8:]
	}
	e.mu.Unlock()
	_ = e.Mem.LogTurn(ctx, pa.ID, m.AuthorID, m.ChannelID, m.Content, final)
	for _, h := range hooks {
		if h.AfterReply != nil {
			h.AfterReply(ctx, m, final)
		}
	}
	e.Tel.Add(telemetry.Record{Kind: "turn", Persona: pa.ID, Channel: m.ChannelID, Provider: resp.Provider, Model: resp.Model,
		Latency: time.Since(start).Seconds(), Words: len(strings.Fields(final)), Robotic: guard.RoboticHits(final), Tools: toolsUsed, Rounds: rounds})
	e.replied(m, cfg, final, resp, time.Since(start), toolsUsed)
	if cfg.Memory.AutoExtract && memoryCandidate(m.Content) {
		if !slicesContains(toolsUsed, "remember") { // the model already saved it on purpose
			go e.extractMemories(context.WithoutCancel(ctx), pa, m)
		}
	}
	return out
}

func (e *Engine) regenerate(ctx context.Context, req provider.Request, nudge string) (string, error) {
	req.Tools = nil
	req.Messages = append(append([]provider.Message(nil), req.Messages...), provider.Message{Role: provider.User, Content: nudge})
	r, err := e.LLM.Complete(ctx, req)
	return r.Text, err
}

func label(name, text string) string {
	if name == "" {
		return text
	}
	return name + ": " + text
}

func languageHint(l string) string {
	if l == "" || l == "auto" {
		return "Reply in English until the person writes in another language; then switch to theirs and stay there."
	}
	return "Default language: " + l + ", until the person writes in another one."
}

// history converts recent platform messages into model roles. Her own lines
// are assistant turns; everyone else is labelled so speakers stay distinct.
func (e *Engine) history(ctx context.Context, m sdk.Message, n int) []provider.Message {
	if n <= 0 {
		n = 20
	}
	hist, err := e.Tr.History(ctx, m.ChannelID, n+1)
	if err != nil {
		return nil
	}
	self := e.Tr.Self().ID
	var out []provider.Message
	for _, h := range hist {
		if h.ID == m.ID || h.Content == "" {
			continue
		}
		if h.AuthorID == self {
			out = append(out, provider.Message{Role: provider.Assistant, Content: h.Content})
		} else {
			out = append(out, provider.Message{Role: provider.User, Content: label(h.AuthorName, h.Content)})
		}
	}
	return out
}

func (e *Engine) deliver(ctx context.Context, m sdk.Message, reason Reason, pa persona.Persona, text string, files []sdk.File, reactions []string) {
	cfg := e.Cfg.Get()
	t := cfg.Behavior.Timing
	if t.ThinkMax > 0 && !m.IsDM {
		e.Sleep(ctx, time.Duration((t.ThinkMin+rand.Float64()*(t.ThinkMax-t.ThinkMin))*float64(time.Second)))
	}
	replyTo := ""
	if !m.IsDM && (reason == ReasonMention || reason == ReasonReply) {
		replyTo = m.ID
	}
	chunks := guard.Split(text, cfg.Behavior.Turn.MaxReplyChars)
	if len(chunks) == 0 {
		chunks = []string{""}
	}
	for i, c := range chunks {
		if c != "" {
			e.Tr.Typing(ctx, m.ChannelID)
			d := min(float64(len(c))*t.TypingPerCh, t.TypingMax)
			e.Sleep(ctx, time.Duration(d*float64(time.Second)))
		}
		r := sdk.Reply{Text: c, ReplyToID: replyTo}
		if i == len(chunks)-1 {
			r.Files, r.Reactions = files, reactions
			if r.ReplyToID == "" && len(reactions) > 0 {
				r.ReplyToID = m.ID // reactions attach to the triggering message
			}
		}
		if err := e.Tr.Send(ctx, m.ChannelID, r); err != nil {
			e.Log.Warn("send failed", "err", err)
			return
		}
		replyTo = "" // only the first chunk quotes
	}
}

func slicesContains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func placeOf(m sdk.Message) string {
	if m.IsDM {
		return "DM"
	}
	return strings.TrimSpace("#" + m.ChannelName + " · " + m.GuildName)
}

func preview(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// heard publishes the decision for one inbound message: the reason she woke
// up, or the reason she did not.
func (e *Engine) heard(m sdk.Message, cfg config.Config, spoke bool, why Reason) {
	if why == ReasonOtherServer {
		return // not her room: not worth a line in the feed
	}
	t := "quiet"
	if spoke {
		t = "heard"
	}
	text := preview(m.Content, 160)
	if cfg.WebUI.HideMessages {
		text = ""
	}
	e.Ev.Emit(events.Event{Type: t, Place: placeOf(m), Author: m.AuthorName, Text: text, Reason: string(why), Why: why.Human()})
}

func (e *Engine) replied(m sdk.Message, cfg config.Config, final string, resp provider.Response, took time.Duration, toolsUsed []string) {
	text := preview(final, 600)
	if cfg.WebUI.HideMessages {
		text = ""
	}
	model := strings.Trim(resp.Provider+" / "+resp.Model, " /")
	e.Ev.Emit(events.Event{Type: "replied", Place: placeOf(m), Text: text, Words: len(strings.Fields(final)),
		Latency: took.Milliseconds(), Model: model, Tools: toolsUsed})
}
