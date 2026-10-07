package engine

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

func (e *Engine) reminderLoop(ctx context.Context) {
	t := time.NewTicker(20 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.FireReminders(ctx, time.Now())
		}
	}
}

// FireReminders delivers due reminders in the companion's own voice.
func (e *Engine) FireReminders(ctx context.Context, now time.Time) {
	due, err := e.Mem.DueReminders(ctx, now)
	if err != nil {
		e.Log.Warn("reminders", "err", err)
		return
	}
	pa, perr := e.personaCfg()
	for _, r := range due {
		text := "reminder: " + r.Content
		if perr == nil {
			sys := persona.Compose(pa, persona.Context{Now: now.Format(time.RFC1123), Extra: []string{"You promised to remind someone. Do it now in one short natural line. The reminder: " + r.Content}})
			if resp, err := e.LLM.Complete(ctx, provider.Request{System: sys, Messages: []provider.Message{{Role: provider.User, Content: "[system: reminder is due]"}}, MaxTokens: 120}); err == nil && strings.TrimSpace(resp.Text) != "" {
				text = strings.TrimSpace(resp.Text)
			}
		}
		_ = e.Tr.Send(ctx, r.ChannelID, sdk.Reply{Text: text})
	}
}

func (e *Engine) maintenanceLoop(ctx context.Context) {
	for {
		h := e.Cfg.Get().Memory.MaintenanceHrs
		if h <= 0 {
			h = 24
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(h) * time.Hour):
			if n, err := e.Mem.Maintain(ctx); err == nil && n > 0 {
				e.Log.Info("memory maintenance", "pruned", n)
			}
		}
	}
}

var factHint = regexp.MustCompile(`(?i)\b(i am|i'm|im|my |i like|i love|i hate|i work|i live|i play|i study|soy |mi |me gusta|odio|trabajo|vivo|juego)\b`)

// memoryCandidate is the cheap gate before spending a model call on
// extraction: long enough and shaped like a first-person statement.
func memoryCandidate(s string) bool {
	return len([]rune(s)) >= 30 && factHint.MatchString(s) && !strings.Contains(s, "http")
}

// extractMemories asks the model for durable facts in the speaker's own
// message. It only ever stores what the person said about themselves, under
// their id.
func (e *Engine) extractMemories(ctx context.Context, pa persona.Persona, m sdk.Message, per memory.Person) {
	ctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	sys := `Extract durable personal facts the speaker states about THEMSELVES (preferences, projects, relationships, routines, things they own). Ignore jokes, opinions about others, anything uncertain. Each fact is one short sentence that starts with the speaker's name (given below), e.g. ["<name> likes Frieren"]. Reply with ONLY a JSON array of strings. Empty array if nothing durable.`
	resp, err := e.LLM.Complete(ctx, provider.Request{System: sys, MaxTokens: 200, Messages: []provider.Message{{Role: provider.User, Content: "Speaker name: " + per.Display() + "\nMessage: " + m.Content}}})
	if err != nil {
		return
	}
	txt := resp.Text
	if i, j := strings.Index(txt, "["), strings.LastIndex(txt, "]"); i >= 0 && j > i {
		txt = txt[i : j+1]
	}
	var facts []string
	if json.Unmarshal([]byte(txt), &facts) != nil {
		return
	}
	for _, f := range facts[:min(len(facts), 4)] {
		if len([]rune(f)) >= 12 {
			_, _ = e.Mem.Remember(ctx, pa.ID, memory.Semantic, per.ID, strings.TrimSpace(f), 0.55, "auto")
		}
	}
}

// Catchup answers what was said while she was offline: direct calls always,
// ambient chatter only through the usual burst collapse, so a restart never
// produces a flood. Needs a transport that implements sdk.Catchup.
func (e *Engine) Catchup(ctx context.Context) {
	cu, ok := e.Tr.(sdk.Catchup)
	if !ok {
		return
	}
	for _, ch := range e.Cfg.Get().Discord.HomeChannels {
		msgs, err := cu.Unseen(ctx, ch, 10)
		if err != nil {
			e.Log.Warn("catchup", "channel", ch, "err", err)
			continue
		}
		for _, m := range msgs {
			go e.Handle(ctx, m)
		}
	}
}
