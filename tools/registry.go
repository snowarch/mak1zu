// Package tools holds the tool registry and the built-in tools. A tool is
// anything implementing sdk.Tool; plugins add more through engine.Use.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/snowarch/mak1zu/sdk"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]sdk.Tool
	// Disabled lists tool names switched off in config (live).
	Disabled func() []string
}

func (r *Registry) off(name string) bool {
	if r.Disabled == nil {
		return false
	}
	for _, d := range r.Disabled() {
		if d == name {
			return true
		}
	}
	return false
}

func NewRegistry() *Registry { return &Registry{tools: map[string]sdk.Tool{}} }

func (r *Registry) Add(t sdk.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[t.Spec().Name] = t
}

func (r *Registry) Get(name string) (sdk.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the specs offered for a turn. Heavy tools are withheld on
// casual turns so chatter never pays for (or triggers) research.
func (r *Registry) Specs(allowHeavy bool) []sdk.ToolSpec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []sdk.ToolSpec
	for _, t := range r.tools {
		if s := t.Spec(); (allowHeavy || !s.Heavy) && !r.off(s.Name) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Call runs a tool and wraps the result as data. Errors become text for the
// model so a failed tool never kills the turn.
func (r *Registry) Call(ctx context.Context, name, args string, env *sdk.CallEnv) string {
	t, ok := r.Get(name)
	if !ok || r.off(name) {
		return fmt.Sprintf("<tool_result name=%q>error: unknown tool</tool_result>", name)
	}
	if args == "" {
		args = "{}"
	}
	res, err := t.Call(ctx, json.RawMessage(args), env)
	if err != nil {
		res = "error: " + err.Error()
	}
	if len(res) > 6000 {
		res = res[:6000] + "\n…(truncated)"
	}
	return fmt.Sprintf("<tool_result name=%q>\n%s\n</tool_result>", name, res)
}

// Schema builds a JSON Schema object from properties: name -> {type,desc}.
func Schema(required []string, props map[string][2]string) json.RawMessage {
	if required == nil {
		required = []string{} // providers reject "required": null
	}
	p := map[string]any{}
	for k, v := range props {
		p[k] = map[string]any{"type": v[0], "description": v[1]}
	}
	b, _ := json.Marshal(map[string]any{"type": "object", "properties": p, "required": required})
	return b
}
