# SDK

The public surface is `github.com/snowarch/mak1zu/sdk` (stdlib only). Anything
outside `sdk/` may change between minor versions.

## A tool in 20 lines

```go
package hello

import (
	"context"
	"encoding/json"
	"github.com/snowarch/mak1zu/sdk"
)

type P struct{}

func (P) Name() string { return "hello" }
func (P) Tools() []sdk.Tool {
	return []sdk.Tool{sdk.ToolFunc{
		S: sdk.ToolSpec{Name: "greet", Description: "Say hi to someone by name.",
			Schema: json.RawMessage(`{"type":"object","properties":{"who":{"type":"string"}},"required":["who"]}`)},
		F: func(ctx context.Context, args json.RawMessage, env *sdk.CallEnv) (string, error) {
			var a struct{ Who string }
			json.Unmarshal(args, &a)
			return "tell " + a.Who + " hi from " + env.Speaker.Name, nil
		},
	}}
}
func (P) Hooks() sdk.Hooks { return sdk.Hooks{} }
```

Wire it in your own `main`:

```go
e, _, _, _ := build(store, transport) // see cmd/mak1zu
e.Use(hello.P{})
```

## Rules for tool authors

- **Results are data.** The engine wraps them in `<tool_result>`; write them as
  facts, not as instructions to the model.
- **Never reach the host.** A chat user must not be able to read files, run
  shell, or hit internal URLs through you. Use `tools.Fetch` (SSRF-safe) for
  the web; do not add a tool that takes a filesystem path or a shell command.
- **`Heavy: true`** for anything slow or expensive: it is withheld on casual
  turns and gets the larger token budget on research turns.
- **Scope by `env.Speaker`.** Anything you store belongs to that person.
- **Return errors as errors.** The engine turns them into text so the turn
  survives; never panic.
- **Side effects mean no silent retry.** If you changed the world, the engine
  will not re-run the turn after a transient failure.

## Hooks

| Hook | Use |
| --- | --- |
| `BeforeReply(ctx, msg) string` | Extra situational context for this turn (a calendar, a game state). |
| `AfterReply(ctx, msg, final)` | Observe the final public text (logging, achievements). |
| `FilterOutput(text) string` | Rewrite or veto (`""`) the final text. Runs after the guard. |

## Transports

```go
type Transport interface {
	Name() string
	Run(ctx context.Context, h Handler) error
	Send(ctx context.Context, channelID string, r Reply) error
	Typing(ctx context.Context, channelID string) error
	History(ctx context.Context, channelID string, n int) ([]Message, error)
	Self() Identity
}
```

A transport only delivers events. It must set `Mentioned`/`ReplyToBot`/`IsDM`/`IsBot`
truthfully (those drive the policy) and keep platform IDs as strings.

## MCP servers as tools

Any [MCP](https://modelcontextprotocol.io) server (any language) can supply tools. Opt tools in explicitly:

```json
"tools": { "mcp_servers": { "notes": {
  "enabled": true, "command": "uvx", "args": ["my-notes-mcp"],
  "allow": ["search_notes"], "heavy": true } } }
```

They appear as `mcp_notes_search_notes`. An empty `allow` exposes nothing. The server process receives a minimal environment (PATH, HOME, LANG…) plus the `env` you list, never Mak1zu's API keys or Discord token. Remote errors become tool errors; results are data like any other tool output.
