package home

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newHome(t *testing.T) Home { return Home{Dir: t.TempDir()} }

func TestDirectivesOrderAndScope(t *testing.T) {
	h := newHome(t)
	h.Write(Rules, "20-tone", "Be kind to newcomers.")
	h.Write(Rules, "10-base", "Never share DMs.")
	h.Write(Rules, "off", "---\nenabled: false\n---\nIgnore me.")
	h.Write(Servers, "111111111111111111", "This server is about Linux ricing.")
	h.Write(Channels, "222222222222222222", "Spoilers go in threads.")
	got := h.Directives("111111111111111111", "222222222222222222")
	if strings.Index(got, "Never share") > strings.Index(got, "Be kind") {
		t.Fatal("rules not sorted by name")
	}
	if strings.Contains(got, "Ignore me") {
		t.Fatal("disabled rule included")
	}
	for _, want := range []string{"Linux ricing", "Spoilers go in threads"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	other := h.Directives("999999999999999999", "888888888888888888")
	if strings.Contains(other, "Linux ricing") || strings.Contains(other, "Spoilers") {
		t.Fatal("scoped rule leaked to another server/channel")
	}
}

func TestSkillsCatalogAndSafeReads(t *testing.T) {
	h := newHome(t)
	h.Write(Skills, "anime-recs", "---\ndescription: recommend anime like a friend\n---\n# body\nAsk what they liked first.")
	os.MkdirAll(filepath.Join(h.Dir, "skills", "anime-recs", "references"), 0o755)
	os.WriteFile(filepath.Join(h.Dir, "skills", "anime-recs", "references", "genres.md"), []byte("slice of life..."), 0o644)
	cat := h.SkillList()
	if len(cat) != 1 || cat[0].Name != "anime-recs" || cat[0].Description != "recommend anime like a friend" {
		t.Fatalf("%+v", cat)
	}
	out, err := h.ReadSkill("anime-recs", "", 0)
	if err != nil || !strings.Contains(out, "Ask what they liked") || !strings.Contains(out, "genres.md") || strings.Contains(out, "description:") {
		t.Fatal(out, err)
	}
	if ref, err := h.ReadSkill("anime-recs", "genres.md", 0); err != nil || !strings.Contains(ref, "slice of life") {
		t.Fatal(ref, err)
	}
	secret := filepath.Join(h.Dir, "config.json")
	os.WriteFile(secret, []byte(`{"token":"SECRET"}`), 0o600)
	for _, bad := range [][2]string{{"../", ""}, {"..", ""}, {"anime-recs", "../../config.json"}, {"anime-recs", "../SKILL.md"}, {"/etc", "passwd"}, {"anime-recs", "genres.md/../../../config.json"}, {"ANIME", ""}} {
		if out, err := h.ReadSkill(bad[0], bad[1], 0); err == nil {
			t.Errorf("read %v -> %q", bad, out)
		}
	}
}

func TestSymlinksAreNeverFollowed(t *testing.T) {
	h := newHome(t)
	secret := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(secret, []byte("TOP SECRET"), 0o600)
	os.MkdirAll(filepath.Join(h.Dir, "rules"), 0o755)
	os.Symlink(secret, filepath.Join(h.Dir, "rules", "leak.md"))
	os.MkdirAll(filepath.Join(h.Dir, "skills", "evil"), 0o755)
	os.Symlink(secret, filepath.Join(h.Dir, "skills", "evil", "SKILL.md"))
	if strings.Contains(h.Directives("", ""), "TOP SECRET") {
		t.Fatal("rule symlink followed")
	}
	if _, err := h.ReadSkill("evil", "", 0); err == nil {
		t.Fatal("skill symlink followed")
	}
	if err := h.Write(Rules, "leak", "overwrite"); err == nil {
		t.Fatal("wrote through a symlink")
	}
	if b, _ := os.ReadFile(secret); string(b) != "TOP SECRET" {
		t.Fatal("symlink target modified")
	}
}

func TestPagination(t *testing.T) {
	h := newHome(t)
	h.Write(Skills, "long", strings.Repeat("a", PageSize+500))
	p1, _ := h.ReadSkill("long", "", 0)
	if !strings.Contains(p1, "offset=") {
		t.Fatal("no continuation hint")
	}
	p2, _ := h.ReadSkill("long", "", PageSize)
	if strings.Contains(p2, "offset=") || len(p2) < 400 {
		t.Fatal(len(p2))
	}
}

func TestWriteValidatesNamesAndSize(t *testing.T) {
	h := newHome(t)
	for _, c := range []struct {
		k Kind
		n string
	}{{Rules, "../x"}, {Rules, "A"}, {Servers, "abc"}, {Skills, "a/b"}, {Channels, "../../etc"}} {
		if h.Write(c.k, c.n, "x") == nil {
			t.Errorf("accepted %v %q", c.k, c.n)
		}
	}
	if h.Write(Rules, "big", strings.Repeat("x", maxFile+1)) == nil || h.Write(Rules, "empty", "  ") == nil {
		t.Fatal("size/empty not enforced")
	}
	if err := h.Write(Rules, "ok", "fine"); err != nil {
		t.Fatal(err)
	}
	if err := h.Delete(Rules, "ok"); err != nil {
		t.Fatal(err)
	}
}
