package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

type fakeTransport struct {
	mu   sync.Mutex
	sent []sdk.Reply
	hist []sdk.Message
}

func (f *fakeTransport) Name() string                           { return "discord" }
func (f *fakeTransport) Run(context.Context, sdk.Handler) error { return nil }
func (f *fakeTransport) Typing(context.Context, string) error   { return nil }
func (f *fakeTransport) Self() sdk.Identity                     { return sdk.Identity{ID: "bot", Name: "Maki"} }
func (f *fakeTransport) History(context.Context, string, int) ([]sdk.Message, error) {
	return f.hist, nil
}
func (f *fakeTransport) Send(_ context.Context, _ string, r sdk.Reply) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, r)
	return nil
}
func (f *fakeTransport) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var o []string
	for _, r := range f.sent {
		o = append(o, r.Text)
	}
	return o
}

type script struct {
	mu    sync.Mutex
	steps []func(provider.Request) (provider.Response, error)
	reqs  []provider.Request
}

func (s *script) Complete(_ context.Context, r provider.Request) (provider.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqs = append(s.reqs, r)
	if len(s.steps) == 0 {
		return provider.Response{}, errors.New("script exhausted")
	}
	f := s.steps[0]
	s.steps = s.steps[1:]
	return f(r)
}
func say(t string) func(provider.Request) (provider.Response, error) {
	return func(provider.Request) (provider.Response, error) {
		return provider.Response{Text: t, Provider: "fake"}, nil
	}
}
func fail(e error) func(provider.Request) (provider.Response, error) {
	return func(provider.Request) (provider.Response, error) { return provider.Response{}, e }
}

func setup(t *testing.T, steps ...func(provider.Request) (provider.Response, error)) (*Engine, *fakeTransport, *script) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "maki"), 0o755)
	os.WriteFile(filepath.Join(dir, "maki", "persona.md"), []byte("---\nname: Maki\n---\nYou are Maki."), 0o644)
	cfg := config.Default()
	cfg.Persona.Dir = dir
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Behavior.Response.BurstSettleSecs = 0
	cfg.Behavior.Timing = config.Timing{}
	cfg.Memory.AutoExtract = false
	mem, err := memory.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	tr := &fakeTransport{}
	sc := &script{steps: steps}
	e := New(config.New(filepath.Join(dir, "c.json"), cfg), sc, mem, persona.Library{Dir: dir}, tr)
	e.Sleep = func(context.Context, time.Duration) {}
	return e, tr, sc
}

// personID is the person an account became; memory is keyed by it, not by the platform id.
func personID(t *testing.T, e *Engine, external string) string {
	t.Helper()
	p, ok, err := e.Mem.Lookup(context.Background(), e.Tr.Name(), external)
	if err != nil || !ok {
		t.Fatalf("no person for account %q: %v", external, err)
	}
	return p.ID
}

func msg(id, content string) sdk.Message {
	return sdk.Message{ID: id, ChannelID: "home", AuthorID: "u1", AuthorName: "Alice", Content: content, Mentioned: true, GuildID: "g", Time: time.Now()}
}

func TestHappyPathReplies(t *testing.T) {
	e, tr, _ := setup(t, say("ok fine, i'm here"))
	e.Handle(context.Background(), msg("1", "hey"))
	if got := tr.texts(); len(got) != 1 || got[0] != "ok fine, i'm here" {
		t.Fatalf("%v", got)
	}
	if st := e.Mem.Stats(context.Background()); st["turns"] != 1 || st["people"] != 1 {
		t.Fatalf("%v", st)
	}
}

func TestDuplicateMessageIDIsIgnored(t *testing.T) {
	e, tr, _ := setup(t, say("one"), say("two"))
	e.Handle(context.Background(), msg("1", "hey"))
	e.Handle(context.Background(), msg("1", "hey"))
	if len(tr.texts()) != 1 {
		t.Fatalf("double reply: %v", tr.texts())
	}
}

func TestToolLoopRunsToolThenSpeaks(t *testing.T) {
	call := func(provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "remember", Args: `{"content":"Alice likes Frieren a lot","kind":"semantic"}`}}}, nil
	}
	e, tr, sc := setup(t, call, say("noted, frieren person"))
	e.Handle(context.Background(), msg("1", "i love frieren, remember it"))
	if got := tr.texts(); len(got) != 1 || got[0] != "noted, frieren person" {
		t.Fatalf("%v", got)
	}
	pid := personID(t, e, "u1")
	ms, _ := e.Mem.Recall(context.Background(), "maki", pid, "frieren", 5)
	if len(ms) != 1 {
		t.Fatalf("memory not saved: %v", ms)
	}
	last := sc.reqs[1].Messages
	if !strings.Contains(last[len(last)-1].Content, `<tool_result name="remember"`) {
		t.Fatalf("tool result must be wrapped as data: %q", last[len(last)-1].Content)
	}
}

func TestProtocolAndLeakNeverReachTheRoom(t *testing.T) {
	e, tr, _ := setup(t,
		say("## How you write\nbe short"), say("<recalled_memories>- secret</recalled_memories>"))
	e.Handle(context.Background(), msg("1", "hey"))
	for _, s := range tr.texts() {
		if strings.Contains(s, "How you write") || strings.Contains(s, "secret") {
			t.Fatalf("leaked: %q", s)
		}
	}
	if len(tr.texts()) != 1 {
		t.Fatalf("expected one failure line, got %v", tr.texts())
	}
}

func TestTransientFailureRetriesOnceThenApologisesInSpanish(t *testing.T) {
	down := fail(&provider.Error{Kind: provider.KindServer, Provider: "x", Status: 502, Msg: "bad gateway"})
	e, tr, sc := setup(t, down, down)
	m := msg("1", "hola che, me ayudás con esto?")
	e.Handle(context.Background(), m)
	if len(sc.reqs) != 2 {
		t.Fatalf("want exactly 1 retry, got %d calls", len(sc.reqs))
	}
	got := tr.texts()
	if len(got) != 1 || !strings.Contains(got[0], "caído") {
		t.Fatalf("%v", got)
	}
	if e.Inc.Block("home") == "" {
		t.Fatal("incident not remembered for the next turn")
	}
}

func TestRetrySucceedsSilently(t *testing.T) {
	down := fail(&provider.Error{Kind: provider.KindTimeout, Provider: "x", Msg: "t/o"})
	e, tr, _ := setup(t, down, say("sorry, back"))
	e.Handle(context.Background(), msg("1", "hey"))
	if got := tr.texts(); len(got) != 1 || got[0] != "sorry, back" {
		t.Fatalf("%v", got)
	}
}

func TestMisconfigurationIsNotRetried(t *testing.T) {
	bad := fail(&provider.Error{Kind: provider.KindAuth, Provider: "x", Status: 401})
	e, tr, sc := setup(t, bad, say("should not happen"))
	e.Handle(context.Background(), msg("1", "hey"))
	if len(sc.reqs) != 1 || !strings.Contains(tr.texts()[0], "misconfigured") {
		t.Fatalf("calls=%d texts=%v", len(sc.reqs), tr.texts())
	}
}

func TestLoopTriggersOneRegeneration(t *testing.T) {
	long := "you really thought that was going to work, huh, the audacity of this man today"
	e, tr, _ := setup(t, say(long), say(long+" again"), say("ok new thought entirely"))
	e.Handle(context.Background(), msg("1", "hey"))
	e.Handle(context.Background(), msg("2", "hey again"))
	got := tr.texts()
	if len(got) != 2 || got[1] != "ok new thought entirely" {
		t.Fatalf("%v", got)
	}
}

func TestUnmetPromiseBecomesIncident(t *testing.T) {
	e, _, _ := setup(t, say("done, i'll remind you tomorrow"))
	e.Handle(context.Background(), msg("1", "remind me about the dock"))
	if !strings.Contains(e.Inc.Block("home"), string(CauseUnmetPromise)) {
		t.Fatal("promise gap not recorded")
	}
}

func TestSpeakerNeverSeesSomeoneElsesMemory(t *testing.T) {
	e, _, sc := setup(t, say("hm"))
	ctx := context.Background()
	e.Mem.Remember(ctx, "maki", memory.Semantic, "bob", "bob secretly collects dakimakura pillows", 0.9, "")
	e.Handle(ctx, msg("1", "what do you know about dakimakura"))
	if strings.Contains(sc.reqs[0].System, "bob secretly") {
		t.Fatal("bob's private memory was put in alice's prompt")
	}
}

func TestPolicyDMOwnerOnlyAndMentionOnlyChannels(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.OwnerID = "owner"
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Discord.MentionOnly = []string{"inir"}
	p := NewPolicy()
	if ok, _ := p.Decide(sdk.Message{IsDM: true, AuthorID: "rando", ChannelID: "dm"}, cfg, "Maki"); ok {
		t.Fatal("stranger DM answered")
	}
	if ok, r := p.Decide(sdk.Message{IsDM: true, AuthorID: "owner", ChannelID: "dm"}, cfg, "Maki"); !ok || r != ReasonDM {
		t.Fatal("owner DM ignored")
	}
	for _, m := range []sdk.Message{
		{ChannelID: "inir", AuthorID: "u", Content: "hey Maki what's up"},
		{ChannelID: "inir", AuthorID: "u", Content: "replying", ReplyToBot: true},
		{ChannelID: "inir", AuthorID: "bot2", IsBot: true, Content: "banter"},
	} {
		if ok, _ := p.Decide(m, cfg, "Maki"); ok {
			t.Fatalf("mention-only channel woke on %+v", m)
		}
	}
	if ok, _ := p.Decide(sdk.Message{ChannelID: "inir", AuthorID: "u", Content: "<@bot> hi", Mentioned: true}, cfg, "Maki"); !ok {
		t.Fatal("real mention ignored")
	}
}

func TestPolicyAmbientOnlyInHomeAndCeiling(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Behavior.Response.HomeAlwaysReply = true
	cfg.Behavior.Response.MaxPerMinute = 3
	p := NewPolicy()
	if ok, _ := p.Decide(sdk.Message{ChannelID: "elsewhere", AuthorID: "u", Content: "anyone?"}, cfg, "Maki"); ok {
		t.Fatal("free-talked outside home")
	}
	n := 0
	for i := 0; i < 6; i++ {
		if ok, _ := p.Decide(sdk.Message{ChannelID: "home", AuthorID: "u", Content: "blah blah blah"}, cfg, "Maki"); ok {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("ceiling not enforced: %d", n)
	}
	// even a direct mention is capped
	if ok, r := p.Decide(sdk.Message{ChannelID: "home", AuthorID: "u", Mentioned: true}, cfg, "Maki"); ok || r != ReasonCeiling {
		t.Fatal("ceiling must hold for direct calls")
	}
}

func TestPolicyPeerBanterBounded(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Discord.PeerBots = []string{"sis"}
	cfg.Behavior.Response.Chances.Peer = 1
	cfg.Behavior.Response.PeerCooldownSecs = 0
	cfg.Behavior.Response.MaxPeerExchanges = 2
	p := NewPolicy()
	var n int
	for i := 0; i < 5; i++ {
		if ok, _ := p.Decide(sdk.Message{ChannelID: "home", AuthorID: "sis", IsBot: true, Content: "hi"}, cfg, "Maki"); ok {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("peer exchanges %d", n)
	}
	if ok, _ := p.Decide(sdk.Message{ChannelID: "home", AuthorID: "stranger-bot", IsBot: true}, cfg, "Maki"); ok {
		t.Fatal("unknown bot answered")
	}
}

func TestFilesAndReactionsAreDelivered(t *testing.T) {
	react := func(provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{{ID: "r", Name: "react", Args: `{"emoji":"💀"}`}}}, nil
	}
	e, tr, _ := setup(t, react, say("lmao"))
	e.Handle(context.Background(), msg("1", "i dropped prod"))
	if len(tr.sent) != 1 || len(tr.sent[0].Reactions) != 1 || tr.sent[0].ReplyToID != "1" {
		t.Fatalf("%+v", tr.sent)
	}
}

func TestPluginToolAndHooks(t *testing.T) {
	e, tr, _ := setup(t, say("fine"))
	e.Use(testPlugin{})
	e.Handle(context.Background(), msg("1", "hey"))
	if got := tr.texts(); len(got) != 1 || got[0] != "FINE" {
		t.Fatalf("FilterOutput hook not applied: %v", got)
	}
	if _, ok := e.Tools.Get("plugin_tool"); !ok {
		t.Fatal("plugin tool missing")
	}
}

type testPlugin struct{}

func (testPlugin) Name() string { return "t" }
func (testPlugin) Tools() []sdk.Tool {
	return []sdk.Tool{sdk.ToolFunc{S: sdk.ToolSpec{Name: "plugin_tool", Schema: json.RawMessage(`{"type":"object"}`)},
		F: func(context.Context, json.RawMessage, *sdk.CallEnv) (string, error) { return "x", nil }}}
}
func (testPlugin) Hooks() sdk.Hooks {
	return sdk.Hooks{FilterOutput: strings.ToUpper}
}

type catchupTransport struct {
	fakeTransport
	unseen []sdk.Message
}

func (c *catchupTransport) Unseen(context.Context, string, int) ([]sdk.Message, error) {
	return c.unseen, nil
}

func TestCatchupAnswersMissedDirectCallOnce(t *testing.T) {
	e, _, _ := setup(t, say("sorry, was out"), say("should not be used"))
	ct := &catchupTransport{unseen: []sdk.Message{msg("m1", "hey you there?")}}
	e.Tr = ct
	e.Cfg.Patch(map[string]any{"discord.home_channels": []any{"home"}})
	e.Catchup(context.Background())
	time.Sleep(100 * time.Millisecond)
	e.Catchup(context.Background()) // a second pass must not double-reply (dedupe by message id)
	time.Sleep(100 * time.Millisecond)
	if got := ct.texts(); len(got) != 1 || got[0] != "sorry, was out" {
		t.Fatalf("%v", got)
	}
}

func findCmd(e *Engine, name string) sdk.Command {
	for _, c := range e.Commands() {
		if c.Name == name {
			return c
		}
	}
	panic(name)
}

func TestCommandsOwnerGateAndPrivateMemory(t *testing.T) {
	e, _, _ := setup(t)
	e.Cfg.Patch(map[string]any{"discord.owner_id": "owner"})
	ctx := context.Background()
	if out := findCmd(e, "persona").Run(ctx, sdk.CommandCall{UserID: "rando", Args: map[string]string{"id": "maki"}}); !strings.Contains(out, "only the owner") {
		t.Fatal(out)
	}
	if out := findCmd(e, "persona").Run(ctx, sdk.CommandCall{UserID: "owner", Args: map[string]string{"id": "ghost"}}); out != "no such persona" {
		t.Fatal(out)
	}
	findCmd(e, "remember").Run(ctx, sdk.CommandCall{UserID: "alice", Args: map[string]string{"note": "alice collects vinyl records"}})
	if out := findCmd(e, "memories").Run(ctx, sdk.CommandCall{UserID: "bob"}); strings.Contains(out, "vinyl") {
		t.Fatal("bob saw alice's memory through /memories")
	}
	if out := findCmd(e, "memories").Run(ctx, sdk.CommandCall{UserID: "alice"}); !strings.Contains(out, "vinyl") {
		t.Fatal(out)
	}
	if out := findCmd(e, "forget").Run(ctx, sdk.CommandCall{UserID: "bob", Args: map[string]string{"what": "#1"}}); out != "no such memory of yours" {
		t.Fatalf("bob deleted alice's memory: %s", out)
	}
	if out := findCmd(e, "forget").Run(ctx, sdk.CommandCall{UserID: "alice", Args: map[string]string{"what": "all"}}); !strings.Contains(out, "gone") {
		t.Fatal(out)
	}
}

func TestHouseRulesAndSkillsReachThePromptScopedCorrectly(t *testing.T) {
	e, _, sc := setup(t, say("ok"), say("ok2"))
	h := e.Home()
	h.Write(home.Rules, "base", "Never spoil endings.")
	h.Write(home.Channels, "424242424242", "Only in this channel: answer in haiku.")
	h.Write(home.Skills, "anime-recs", "---\ndescription: recommend anime like a friend\n---\nAsk what they loved first.")
	m := msg("1", "hey")
	m.ChannelID = "424242424242"
	e.Cfg.Patch(map[string]any{"discord.home_channels": []any{"424242424242", "home"}})
	e.Handle(context.Background(), m)
	sys := sc.reqs[0].System
	for _, want := range []string{"Never spoil endings.", "answer in haiku", "anime-recs: recommend anime like a friend", "read_skill"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	e.Handle(context.Background(), msg("2", "hey again")) // channel "home"
	if strings.Contains(sc.reqs[1].System, "haiku") {
		t.Fatal("channel rule leaked into another channel")
	}
}

func TestReadSkillToolIsConfinedToSkills(t *testing.T) {
	e, _, _ := setup(t)
	e.Home().Write(home.Skills, "one", "body text")
	os.WriteFile(filepath.Join(e.Home().Dir, "secret.txt"), []byte("SECRET"), 0o600)
	call := func(args string) string {
		return e.Tools.Call(context.Background(), "read_skill", args, &sdk.CallEnv{})
	}
	if out := call(`{"name":"one"}`); !strings.Contains(out, "body text") {
		t.Fatal(out)
	}
	for _, bad := range []string{`{"name":"../"}`, `{"name":"one","file":"../../secret.txt"}`, `{"name":"one","file":"/etc/passwd"}`} {
		if out := call(bad); strings.Contains(out, "SECRET") || strings.Contains(out, "root:") {
			t.Fatalf("escaped via %s: %s", bad, out)
		}
	}
}

func TestGIFLinkGoesAfterTheTextNeverInsteadOfIt(t *testing.T) {
	gif := func(provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{{ID: "g", Name: "nope_tool", Args: `{}`}}}, nil
	}
	e, tr, _ := setup(t, gif, say("bonk. no."))
	e.Tools.Add(sdk.ToolFunc{S: sdk.ToolSpec{Name: "nope_tool", Schema: json.RawMessage(`{"type":"object"}`)},
		F: func(_ context.Context, _ json.RawMessage, env *sdk.CallEnv) (string, error) {
			env.AttachLink("https://nekos.best/api/v2/bonk/a.gif")
			env.AttachLink("https://nekos.best/api/v2/bonk/b.gif") // only one per reply
			return "queued", nil
		}})
	e.Handle(context.Background(), msg("1", "pet me"))
	got := tr.texts()
	if len(got) != 2 || got[0] != "bonk. no." || got[1] != "https://nekos.best/api/v2/bonk/a.gif" {
		t.Fatalf("%v", got)
	}
}

// Providers validate tool schemas strictly: a null "required" or a missing
// "properties" was rejected by a real endpoint (400) and turned every turn
// into a failure. Every registered tool must ship a well-formed schema.
func TestEveryToolSchemaIsStrictlyWellFormed(t *testing.T) {
	e, _, _ := setup(t)
	for _, spec := range e.Tools.Specs(true) {
		var s map[string]any
		if err := json.Unmarshal(spec.Schema, &s); err != nil {
			t.Fatalf("%s: %v", spec.Name, err)
		}
		if s["type"] != "object" {
			t.Errorf("%s: type %v", spec.Name, s["type"])
		}
		if _, ok := s["properties"].(map[string]any); !ok {
			t.Errorf("%s: properties must be an object, got %v", spec.Name, s["properties"])
		}
		if r, present := s["required"]; present {
			if _, ok := r.([]any); !ok {
				t.Errorf("%s: required must be an array, got %v", spec.Name, r)
			}
		}
		if spec.Description == "" {
			t.Errorf("%s: no description", spec.Name)
		}
	}
}

func TestBigBudgetForRealRequestsNotChatter(t *testing.T) {
	for _, yes := range []string{
		"what's frieren's score on anilist?", "write me a tiny html page", "hazme un archivo json", "search for qwen benchmarks",
		"https://example.com/x", "how many episodes does bocchi have", "armame un script en py",
		// the requests that failed in the playground, plus the accent trap (Go's \b is ASCII-only)
		"buscame algun wallpaper cool", "podrias hacerme una homepage en .html y damrela", "búscame un fondo de pantalla",
		"armá una página", "creá un archivo", "haceme un script", "send me a wallpaper", "pasame un fondo de escritorio",
	} {
		if !heavyRe.MatchString(yes) {
			t.Errorf("should get the big budget: %q", yes)
		}
	}
	for _, no := range []string{
		"hey", "i love that page of the manga", "lol the file is huge", "me aburro", "you're annoying", "that episode wrecked me",
		"i generally love that page", "i like programming in go", "no puedo creer esa página", "busco trabajo", "dame un consejo", "el arma de ese personaje",
	} {
		if heavyRe.MatchString(no) {
			t.Errorf("chatter must not get the big budget: %q", no)
		}
	}
}

func TestEveryToolIsOfferedOnCasualTurns(t *testing.T) {
	// whether she can search or make a file must not depend on the wording of the request
	e, _, _ := setup(t)
	var names []string
	for _, s := range e.Tools.Specs(true) {
		names = append(names, s.Name)
	}
	for _, want := range []string{"web_search", "wallpaper", "write_file", "read_url", "anime_search", "reaction_gif"} {
		if !slicesContains(names, want) {
			t.Errorf("%s is not available: %v", want, names)
		}
	}
}

func TestOnlyGuildsKeepsHerOutOfOtherServers(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.OnlyGuilds = []string{"mine"}
	cfg.Discord.OwnerID = "owner"
	p := NewPolicy()
	if ok, _ := p.Decide(sdk.Message{GuildID: "other", ChannelID: "c", AuthorID: "u", Content: "hi Maki", Mentioned: true}, cfg, "Maki"); ok {
		t.Fatal("answered a mention in a server outside only_guilds")
	}
	if ok, _ := p.Decide(sdk.Message{GuildID: "mine", ChannelID: "c", AuthorID: "u", Content: "hi", Mentioned: true}, cfg, "Maki"); !ok {
		t.Fatal("ignored her own server")
	}
	if ok, _ := p.Decide(sdk.Message{IsDM: true, AuthorID: "owner", ChannelID: "d"}, cfg, "Maki"); !ok {
		t.Fatal("owner DM must not depend on only_guilds")
	}
}

func TestWebhooksAreBotsUnlessListedAsHuman(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Discord.HumanWebhooks = []string{"wh1"}
	p := NewPolicy()
	base := sdk.Message{ChannelID: "home", AuthorID: "wh-user", IsBot: true, Mentioned: true, Content: "hi"}
	stranger := base
	stranger.WebhookID = "other"
	if ok, _ := p.Decide(stranger, cfg, "Maki"); ok {
		t.Fatal("unknown webhook treated as a person")
	}
	bridged := base
	bridged.WebhookID = "wh1"
	if ok, r := p.Decide(bridged, cfg, "Maki"); !ok || r != ReasonMention {
		t.Fatal("listed webhook not treated as a person")
	}
}
