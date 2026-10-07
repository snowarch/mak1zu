package engine

import (
	"encoding/json"
	"io/fs"
	"testing"

	makizu "github.com/snowarch/mak1zu"
	"github.com/snowarch/mak1zu/persona"
)

// Budgets: what every single turn costs before the person has said a word.
// They exist so the prompt cannot grow by accretion. Raising one is a decision
// to make on purpose, in a diff someone reads, not a side effect of a feature.
const (
	toolSchemaBudget = 7600  // bytes of tool definitions sent every turn (~1.9k tokens)
	basePromptBudget = 12200 // bytes of persona + shared substrate (~3k tokens)
)

func TestEveryTurnStaysInsideItsBudget(t *testing.T) {
	e, _, _ := setup(t)
	tot := 0
	for _, s := range e.Tools.Specs(true) {
		b, _ := json.Marshal(map[string]any{"name": s.Name, "description": s.Description, "parameters": json.RawMessage(s.Schema)})
		tot += len(b)
	}
	if tot > toolSchemaBudget {
		t.Errorf("tool definitions are %d bytes (budget %d): shorten descriptions or drop a tool", tot, toolSchemaBudget)
	}
	body, err := fs.ReadFile(makizu.Defaults, ".makizu/personas/maki/persona.md")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(body) + len(persona.Substrate); n > basePromptBudget {
		t.Errorf("persona + substrate are %d bytes (budget %d)", n, basePromptBudget)
	}
	t.Logf("tools %d/%d bytes, base prompt %d/%d bytes", tot, toolSchemaBudget, len(body)+len(persona.Substrate), basePromptBudget)
}
