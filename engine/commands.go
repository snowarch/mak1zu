package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/sdk"
)

// Commands returns the platform commands. OwnerOnly is enforced here, not in
// the transport, so every platform gets the same rule.
func (e *Engine) Commands() []sdk.Command {
	owner := func(c sdk.CommandCall) bool {
		o := e.Cfg.Get().Discord.OwnerID
		return o == "" || c.UserID == o
	}
	guardOwner := func(f func(context.Context, sdk.CommandCall) string) func(context.Context, sdk.CommandCall) string {
		return func(ctx context.Context, c sdk.CommandCall) string {
			if !owner(c) {
				return "only the owner can do that"
			}
			return f(ctx, c)
		}
	}
	return []sdk.Command{
		{Name: "ping", Description: "Check she is alive", Run: func(context.Context, sdk.CommandCall) string { return "pong" }},
		{Name: "persona", Description: "Switch the active character (owner)", OwnerOnly: true,
			Options: []sdk.CommandOption{{Name: "id", Description: "persona id", Required: true, Choices: e.personaIDs()}},
			Run: guardOwner(func(_ context.Context, c sdk.CommandCall) string {
				id := c.Args["id"]
				if _, err := e.personaLib().Load(id); err != nil {
					return "no such persona"
				}
				if err := e.Cfg.Patch(map[string]any{"persona.active": id}); err != nil {
					return "could not switch: " + err.Error()
				}
				return "now speaking as " + id
			})},
		{Name: "mood", Description: "How is she feeling?", Run: func(context.Context, sdk.CommandCall) string {
			if m := e.mood.Describe(); m != "" {
				return m
			}
			return "baseline, nothing special"
		}},
		{Name: "remember", Description: "Tell her something to remember about you",
			Options: []sdk.CommandOption{{Name: "note", Description: "what to remember", Required: true}},
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				note := strings.TrimSpace(c.Args["note"])
				if len([]rune(note)) < 8 || len(note) > 400 {
					return "give me a sentence (8 to 400 characters)"
				}
				pa := e.Cfg.Get().Persona.Active
				if _, err := e.Mem.Remember(ctx, pa, memory.Semantic, c.UserID, note, 0.7, "command"); err != nil {
					return "could not save that"
				}
				return "noted"
			}},
		{Name: "memories", Description: "Show what she remembers about you (only you can see this)",
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				ms, _ := e.Mem.Recall(ctx, e.Cfg.Get().Persona.Active, c.UserID, "", 10)
				if len(ms) == 0 {
					return "nothing yet"
				}
				var b strings.Builder
				for _, m := range ms {
					fmt.Fprintf(&b, "#%d %s\n", m.ID, m.Content)
				}
				return b.String()
			}},
		{Name: "forget", Description: "Forget one memory by number, or everything about you with `all`",
			Options: []sdk.CommandOption{{Name: "what", Description: "memory number or `all`", Required: true}},
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				w := strings.TrimSpace(strings.ToLower(c.Args["what"]))
				if w == "all" {
					if err := e.Mem.ForgetUser(ctx, c.UserID); err != nil {
						return "could not erase"
					}
					return "everything about you is gone"
				}
				id, err := strconv.ParseInt(strings.TrimPrefix(w, "#"), 10, 64)
				if err != nil {
					return "give me a memory number or `all`"
				}
				if ok, _ := e.Mem.Forget(ctx, c.UserID, id); ok {
					return "forgotten"
				}
				return "no such memory of yours"
			}},
	}
}

func (e *Engine) personaLib() personaLibrary {
	l := e.Lib
	if l.Dir == "" {
		l.Dir = e.Cfg.Abs(e.Cfg.Get().Persona.Dir)
	}
	return l
}

func (e *Engine) personaIDs() []string { return e.personaLib().List() }
