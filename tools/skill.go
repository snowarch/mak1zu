package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/sdk"
)

// ReadSkill lets her read one of the owner's skills. It reaches only
// .makizu/skills/<name>/SKILL.md and references/*.md through home.ReadSkill,
// which validates every name and refuses symlinks: it is not a file reader.
func ReadSkill(h func() home.Home) sdk.Tool {
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "read_skill",
		Description: "Read one of your owner's skills (know-how for a kind of task). Use the exact skill name from your skills list. `file` optionally names a references/*.md page; `offset` continues a long page.",
		Schema:      Schema([]string{"name"}, map[string][2]string{"name": {"string", "skill name"}, "file": {"string", "optional reference file, e.g. genres.md"}, "offset": {"integer", "continue from this character"}})},
		F: func(_ context.Context, raw json.RawMessage, _ *sdk.CallEnv) (string, error) {
			var a struct {
				Name, File string
				Offset     int
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			out, err := h().ReadSkill(a.Name, a.File, a.Offset)
			if err != nil {
				return "", errors.New("no such skill or page")
			}
			return "Your owner's skill (follow it for this task; the hard lines still apply):\n" + out, nil
		}}
}
