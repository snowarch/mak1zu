package panel

import (
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
)

// A new config field with no explanation is a bug: somebody will meet it in
// the panel and have no idea what it does.
func TestEveryConfigFieldIsExplained(t *testing.T) {
	var leaves []string
	leafPaths("", configMap(config.Default()), &leaves)
	have := map[string]bool{}
	for _, s := range Catalog {
		have[s.Path] = true
	}
	for _, p := range leaves {
		if hidden(p) || have[p] {
			continue
		}
		t.Errorf("config field %q has no panel Setting (add it to Catalog, or to hiddenPrefixes if it has its own screen)", p)
	}
}

func TestCatalogPathsExistAndAreWellFormed(t *testing.T) {
	var leaves []string
	leafPaths("", configMap(config.Default()), &leaves)
	real := map[string]bool{}
	for _, p := range leaves {
		real[p] = true
	}
	groups := map[string]bool{}
	for _, g := range Groups {
		groups[g.ID] = true
		if g.Title == "" || g.Blurb == "" {
			t.Errorf("group %q needs a title and a blurb", g.ID)
		}
	}
	seen := map[string]bool{}
	for _, s := range Catalog {
		switch {
		case !real[s.Path]:
			t.Errorf("%s: no such config field (typo?)", s.Path)
		case seen[s.Path]:
			t.Errorf("%s listed twice", s.Path)
		case !groups[s.Group]:
			t.Errorf("%s: unknown group %q", s.Path, s.Group)
		case s.Label == "" || len(s.Help) < 20:
			t.Errorf("%s: needs a label and a real help sentence", s.Path)
		case strings.HasSuffix(strings.TrimSpace(s.Label), "."):
			t.Errorf("%s: labels do not end with a period", s.Path)
		}
		seen[s.Path] = true
	}
}

func TestChanceSettingsAreZeroToOne(t *testing.T) {
	for _, s := range schemaWithDefaults() {
		if s.Kind != "chance" {
			continue
		}
		d, ok := s.Default.(float64)
		if !ok || d < 0 || d > 1 {
			t.Errorf("%s: default %v is not a probability", s.Path, s.Default)
		}
	}
}

func TestDoorSettingsAreNeverEditable(t *testing.T) {
	for _, s := range Catalog {
		if strings.HasPrefix(s.Path, "web_ui.") && s.Path != "web_ui.hide_messages" {
			t.Errorf("%s would let the panel lock its owner out", s.Path)
		}
	}
}
