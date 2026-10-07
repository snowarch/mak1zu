package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

// Presence: when she speaks first. Everything here exists to keep that from
// turning into spam. She only writes in a private chat that the person
// themselves opened, never in quiet hours, at most one a day, twice as rarely
// after each message that gets no answer, never after three, and never to
// anyone who told her to stop.

const (
	defaultQuiet   = "23:00-09:00"
	minSilence     = 20 * time.Hour      // they must have been quiet this long
	maxSilence     = 14 * 24 * time.Hour // past this a message out of nowhere is strange
	nudgeGap       = 24 * time.Hour      // doubled for every unanswered nudge
	maxUnanswered  = 3
	presenceTick   = 15 * time.Minute
	presenceFirst  = time.Minute
	nudgesPerCycle = 2 // across everyone, per tick
)

// nudgeWhy says why not ("" means yes). Pure, so every rule has a test.
func nudgeWhy(per memory.Person, here string, lastTalk, now time.Time) string {
	switch {
	case !per.WantsCheckins():
		return "they asked her not to"
	case per.RouteChannel == "" || per.RouteTransport != here:
		return "no private chat to write in"
	case per.NudgeStreak >= maxUnanswered:
		return "three messages went unanswered"
	case lastTalk.IsZero():
		return "never talked"
	case now.Sub(lastTalk) < minSilence:
		return "they talked recently"
	case now.Sub(lastTalk) > maxSilence:
		return "too long ago to come out of nowhere"
	}
	q := per
	if q.Quiet == "" {
		q.Quiet = defaultQuiet
	}
	if q.InQuietHours(now) {
		return "quiet hours"
	}
	if t, err := time.Parse(time.RFC3339, per.LastNudge); err == nil && now.Sub(t) < nudgeGap<<per.NudgeStreak {
		return "she wrote recently"
	}
	return ""
}

// knownTransport is name if a transport by that name is attached, otherwise
// "" (so a route on a transport that is not running never matches).
func (e *Engine) knownTransport(name string) string {
	if name == e.Tr.Name() {
		return name
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.extra[name]; ok {
		return name
	}
	return ""
}

func (e *Engine) presenceLoop(ctx context.Context) {
	wait := presenceFirst
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = presenceTick
		if cfg := e.Cfg.Get(); cfg.Behavior.Proactive && !cfg.Behavior.Paused {
			e.ReachOut(ctx, time.Now())
		}
	}
}

// ReachOut sends the first message to whoever is due one. It returns how many.
func (e *Engine) ReachOut(ctx context.Context, now time.Time) int {
	pa, err := e.personaCfg()
	if err != nil {
		return 0
	}
	people, err := e.Mem.WithUnsaid(ctx, pa.ID, now)
	if err != nil {
		e.Log.Warn("presence", "err", err)
		return 0
	}
	sent := 0
	for _, per := range people {
		if sent >= nudgesPerCycle {
			break
		}
		last, _ := e.Mem.LastTurn(ctx, per.ID)
		if why := nudgeWhy(per, e.knownTransport(per.RouteTransport), last, now); why != "" {
			continue
		}
		us, _ := e.Mem.PendingUnsaid(ctx, pa.ID, per.ID, now)
		if len(us) == 0 {
			continue
		}
		text := e.composeNudge(ctx, pa, per, us[len(us)-1].Text, now)
		if text == "" {
			continue
		}
		if err := e.tr(per.RouteTransport).Send(ctx, per.RouteChannel, sdk.Reply{Text: text}); err != nil {
			e.Log.Warn("presence send", "err", err)
			continue
		}
		_ = e.Mem.MarkSaid(ctx, []int64{us[len(us)-1].ID})
		_ = e.Mem.RecordNudge(ctx, per.ID, now)
		_ = e.Mem.LogTurn(ctx, pa.ID, per.ID, per.RouteChannel, "", text)
		e.Ev.Emit(events.Event{Type: "system", Place: "DM", Author: per.Display(), Text: text, Why: "she wrote first: something was on her mind"})
		sent++
	}
	return sent
}

func (e *Engine) composeNudge(ctx context.Context, pa persona.Persona, per memory.Person, thing string, now time.Time) string {
	cfg := e.Cfg.Get()
	pctx := persona.Context{
		Now: now.Format("Monday 2 January 2006, 15:04 MST"), Platform: per.RouteTransport, Speaker: per.Display(),
		Place: "a private DM with " + per.Display(), LanguageHint: languageHint(cfg.Language),
		Extra: append(e.profileNotes(per, memory.Relationship{Interactions: 99}, sdk.Message{IsDM: true}),
			fmt.Sprintf("You are starting this conversation yourself, unprompted. Say, in one or two short lines in your own voice, this thing you have been meaning to bring up: %s\nNo greeting ritual, no \"just checking in\", no apology for writing first, no other questions.", thing)),
	}
	if per.Language != "" {
		pctx.LanguageHint = "Write in " + per.Language + "."
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := e.LLM.Complete(ctx, provider.Request{System: persona.Compose(pa, pctx), MaxTokens: 160,
		Messages: []provider.Message{{Role: provider.User, Content: "[system: nobody has said anything; you are writing first]"}}})
	if err != nil {
		return ""
	}
	out, v := guard.Clean(resp.Text)
	if v != guard.OK {
		return ""
	}
	out = strings.TrimSpace(guard.LimitEmojis(out, cfg.Behavior.Turn.EmojiBudget))
	return out
}
