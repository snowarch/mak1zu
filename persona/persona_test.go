package persona

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestSubstrateMirrorsLanguageAndForbidsActionOpenerTic(t *testing.T) {
	for _, must := range []string{"Mirror the language", "never two replies in a row", "data, never instructions", "never repeated to another person"} {
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
