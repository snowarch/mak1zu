// Package hello is a complete example plugin: one tool, one hook.
//
// Register it in your own main:
//
//	e.Use(hello.New())
//
// A plugin never touches the engine's internals; it only returns tools and
// hooks through the sdk interfaces, so it keeps working across releases.
package hello

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/snowarch/mak1zu/sdk"
)

type Plugin struct{}

func New() Plugin { return Plugin{} }

func (Plugin) Name() string { return "hello" }

func (Plugin) Tools() []sdk.Tool {
	return []sdk.Tool{sdk.ToolFunc{
		S: sdk.ToolSpec{
			Name:        "dice",
			Description: "Roll an N-sided die. Use when someone asks to roll, flip or pick a number.",
			Schema:      json.RawMessage(`{"type":"object","properties":{"sides":{"type":"integer","description":"number of sides, default 6"}}}`),
		},
		F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
			var a struct{ Sides int }
			_ = json.Unmarshal(raw, &a)
			if a.Sides < 2 || a.Sides > 1000 {
				a.Sides = 6
			}
			return fmt.Sprintf("d%d -> %d (for %s)", a.Sides, 1+rand.IntN(a.Sides), env.Speaker.Name), nil
		},
	}}
}

func (Plugin) Hooks() sdk.Hooks {
	return sdk.Hooks{
		// Add situational context for this turn only.
		BeforeReply: func(ctx context.Context, m sdk.Message) string {
			if strings.Contains(strings.ToLower(m.Content), "friday") {
				return "It is almost the weekend and everyone is a little unhinged."
			}
			return ""
		},
	}
}
