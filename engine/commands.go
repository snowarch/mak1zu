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
	owner := func(ctx context.Context, c sdk.CommandCall) bool {
		if e.account(ctx, c.UserID, c.UserName).IsOwner() {
			return true
		}
		return !e.Mem.HasOwner(ctx) && e.Cfg.Get().Discord.OwnerID == "" // nobody holds the role: single-user install
	}
	guardOwner := func(f func(context.Context, sdk.CommandCall) string) func(context.Context, sdk.CommandCall) string {
		return func(ctx context.Context, c sdk.CommandCall) string {
			if !owner(ctx, c) {
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
				if _, err := e.Mem.Remember(ctx, pa, memory.Semantic, e.account(ctx, c.UserID, c.UserName).ID, note, 0.7, "command"); err != nil {
					return "could not save that"
				}
				return "noted"
			}},
		{Name: "memories", Description: "Show what she remembers about you (only you can see this)",
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				ms, _ := e.Mem.Recall(ctx, e.Cfg.Get().Persona.Active, e.account(ctx, c.UserID, c.UserName).ID, "", 10)
				if len(ms) == 0 {
					return "nothing yet"
				}
				var b strings.Builder
				for _, m := range ms {
					fmt.Fprintf(&b, "#%d %s\n", m.ID, m.Content)
				}
				return b.String()
			}},
		{Name: "callme", Description: "Tell her what to call you, on every platform you use",
			Options: []sdk.CommandOption{{Name: "name", Description: "what she should call you (empty to reset)", Required: false}},
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				per := e.account(ctx, c.UserID, c.UserName)
				n := strings.TrimSpace(c.Args["name"])
				if err := e.Mem.SetProfile(ctx, per.ID, "call_me", n); err != nil {
					return err.Error()
				}
				if n == "" {
					return "back to " + per.Name
				}
				return "okay, " + n
			}},
		{Name: "link", Description: "Join this account to you elsewhere (terminal, other platform): run with no code to get one, or paste a code",
			Options: []sdk.CommandOption{{Name: "code", Description: "a code from another platform", Required: false}},
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				per := e.account(ctx, c.UserID, c.UserName)
				if code := strings.TrimSpace(c.Args["code"]); code != "" {
					p, err := e.Mem.Link(ctx, e.Tr.Name(), c.UserID, c.UserName, code)
					if err != nil {
						return err.Error()
					}
					return "linked: you are " + p.Display() + " here too, one memory"
				}
				code, err := e.Mem.NewLinkCode(ctx, per.ID)
				if err != nil {
					return "could not make a code"
				}
				return "code " + code + " (10 minutes, one use). On the other account say /link " + code + " or run `mak1zu link " + code + "` on the machine."
			}},
		{Name: "forget", Description: "Forget one memory by number, or everything about you with `all`",
			Options: []sdk.CommandOption{{Name: "what", Description: "memory number or `all`", Required: true}},
			Run: func(ctx context.Context, c sdk.CommandCall) string {
				w := strings.TrimSpace(strings.ToLower(c.Args["what"]))
				if w == "all" {
					if err := e.Mem.ForgetUser(ctx, e.account(ctx, c.UserID, c.UserName).ID); err != nil {
						return "could not erase"
					}
					return "everything about you is gone"
				}
				id, err := strconv.ParseInt(strings.TrimPrefix(w, "#"), 10, 64)
				if err != nil {
					return "give me a memory number or `all`"
				}
				if ok, _ := e.Mem.Forget(ctx, e.account(ctx, c.UserID, c.UserName).ID, id); ok {
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
