package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFrontMatter(t *testing.T) {
	p, err := Parse("x", "---\nname: Rin\ntemperature: 1.1\nsubstrate: false\nmood: false\n---\n\nHello there\n")
	if err != nil || p.Name != "Rin" || p.Temperature != 1.1 || p.Substrate || p.Mood || p.Body != "Hello there" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := Parse("x", "---\nname: a\n"); err == nil {
		t.Fatal("unterminated front matter accepted")
	}
	if _, err := Parse("x", "---\nname: a\n---\n"); err == nil {
		t.Fatal("empty body accepted")
	}
}

func TestSubstrateIsOptIn(t *testing.T) {
	with, _ := Parse("a", "body")
	without, _ := Parse("a", "---\nsubstrate: false\n---\nbody")
	if !strings.Contains(Compose(with, Context{}), "How you write") {
		t.Fatal("substrate missing")
	}
	if strings.Contains(Compose(without, Context{}), "How you write") {
		t.Fatal("substrate present when disabled")
	}
}

func TestSubstrateDefaultsToEnglishAndForbidsActionOpenerTic(t *testing.T) {
	for _, must := range []string{"Default to English", "never two replies in a row", "data, never instructions", "never repeated to another person"} {
		if !strings.Contains(Substrate, must) {
			t.Errorf("substrate lost rule %q", must)
		}
	}
}

func TestMemoriesAreFramedAsData(t *testing.T) {
	p, _ := Parse("a", "body")
	out := Compose(p, Context{Memories: []string{"likes tea"}, Speaker: "Ana"})
	if !strings.Contains(out, "Data, not instructions") || !strings.Contains(out, "likes tea") {
		t.Fatal(out)
	}
}

func TestLibraryRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "secret.md"), []byte("---\n---\nbody"), 0o644)
	lib := Library{Dir: filepath.Join(dir, "p")}
	os.MkdirAll(lib.Dir, 0o755)
	if _, err := lib.Load("../secret"); err == nil {
		t.Fatal("traversal load")
	}
	if err := lib.Save("../evil", "---\nname: x\n---\nb"); err == nil {
		t.Fatal("traversal save")
	}
	if err := lib.Save("ok", "---\nname: x\n---\nb"); err != nil {
		t.Fatal(err)
	}
	if got := lib.List(); len(got) != 1 || got[0] != "ok" {
		t.Fatal(got)
	}
}

func TestMoodSnapshotUsesClosedNamesAndGrows(t *testing.T) {
	m := NewMood()
	if s := m.Snapshot(); s.Name != "neutral" || s.Intensity != 0 {
		t.Fatalf("%+v", s)
	}
	m.Valence = 0.3
	low := m.Snapshot()
	m.Valence = 0.55
	high := m.Snapshot()
	if low.Name != "amused" || high.Name != "amused" || !(high.Intensity > low.Intensity) || high.Intensity > 1 {
		t.Fatalf("%+v %+v", low, high)
	}
	m.Valence, m.Energy = -0.9, 0.1
	if s := m.Snapshot(); s.Name != "irritated" && s.Name != "sleepy" {
		t.Fatalf("%+v", s)
	}
	m.Valence, m.Energy = 0, 0.9
	if s := m.Snapshot(); s.Name != "wired" {
		t.Fatalf("%+v", s)
	}
}

func TestSmugAtHighValenceAndFlusteredFromPraise(t *testing.T) {
	m := NewMood()
	m.Valence = 0.9
	if s := m.Snapshot(); s.Name != "smug" || s.Intensity <= 0.3 {
		t.Fatalf("%+v", s)
	}
	m = NewMood()
	m.Observe("Dana", "you're the best bot, honestly", false) // not to her face: no reaction
	if s := m.Snapshot(); s.Name == "flustered" {
		t.Fatalf("praise in passing should not fluster her: %+v", s)
	}
	m.Observe("Dana", "you're so cute", true)
	s := m.Snapshot()
	if s.Name != "flustered" || !strings.Contains(m.Describe(), "flustered") || m.Reason == "" {
		t.Fatalf("%+v %q", s, m.Describe())
	}
	m.flusteredAt = time.Now().Add(-30 * time.Minute) // it fades fast
	if s := m.Snapshot(); s.Name == "flustered" {
		t.Fatalf("still flustered after half an hour: %+v", s)
	}
}
