package pack

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/home"
)

const personaText = "---\nname: Rin\n---\nYou are Rin. Calm."

func mk(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestInspectFindsThePersonaWhereverItSits(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"in the folder":     {"persona.md": personaText},
		"in personas/<id>":  {"personas/rin/persona.md": personaText},
		"in its one subdir": {"rin/persona.md": personaText},
	} {
		p, err := Inspect(mk(t, files))
		if err != nil || p.Persona != personaText {
			t.Fatalf("%s: %v", name, err)
		}
	}
	p, _ := Inspect(mk(t, map[string]string{"personas/rin/persona.md": personaText}))
	if p.ID != "rin" {
		t.Fatal(p.ID)
	}
	if _, err := Inspect(mk(t, map[string]string{"readme.md": "x"})); err == nil {
		t.Fatal("a folder with no persona was accepted")
	}
	if _, err := Inspect(mk(t, map[string]string{"personas/a/persona.md": personaText, "personas/b/persona.md": personaText})); err == nil {
		t.Fatal("two characters in one pack were accepted")
	}
}

func TestInspectRefusesWhatIsNotPlainSafeText(t *testing.T) {
	root := mk(t, map[string]string{"persona.md": personaText})
	os.Symlink("/etc/passwd", filepath.Join(root, "link.md"))
	if _, err := Inspect(root); err == nil || !strings.Contains(err.Error(), "link") {
		t.Fatalf("symlink accepted: %v", err)
	}
	if _, err := Inspect(mk(t, map[string]string{"persona.md": personaText, "big.txt": strings.Repeat("x", 70<<10)})); err == nil {
		t.Fatal("oversized file accepted")
	}
	if _, err := Inspect(mk(t, map[string]string{"persona.md": personaText, "skills/Bad Name/SKILL.md": "---\ndescription: x\n---\n"})); err == nil {
		t.Fatal("a skill with a bad name was accepted")
	}
	if _, err := Inspect(mk(t, map[string]string{"persona.md": personaText, "skills/ok/SKILL.md": "no description here"})); err == nil {
		t.Fatal("a skill without a description was accepted")
	}
	if _, err := Inspect(mk(t, map[string]string{"persona.md": personaText, "rules/a b.md": "x"})); err == nil {
		t.Fatal("a rule with a bad name was accepted")
	}
}

func TestUntarCannotWriteOutsideOrCreateLinks(t *testing.T) {
	tarOf := func(h tar.Header, body string) *bytes.Buffer {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		h.Size = int64(len(body))
		tw.WriteHeader(&h)
		tw.Write([]byte(body))
		tw.Close()
		gz.Close()
		return &buf
	}
	dst := t.TempDir()
	if err := Untar(tarOf(tar.Header{Name: "../escape.md", Mode: 0o644, Typeflag: tar.TypeReg}, "x"), dst); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := Untar(tarOf(tar.Header{Name: "/abs.md", Mode: 0o644, Typeflag: tar.TypeReg}, "x"), dst); err == nil {
		t.Fatal("absolute path accepted")
	}
	if err := Untar(tarOf(tar.Header{Name: "l", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}, ""), dst); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := Untar(tarOf(tar.Header{Name: "big", Mode: 0o644, Typeflag: tar.TypeReg}, strings.Repeat("x", 70<<10)), dst); err == nil {
		t.Fatal("oversized entry accepted")
	}
	if err := Untar(tarOf(tar.Header{Name: "ok/persona.md", Mode: 0o644, Typeflag: tar.TypeReg}, personaText), dst); err != nil {
		t.Fatal(err)
	}
}

func TestExportThenInstallRoundTripsAndNeverOverwritesBlindly(t *testing.T) {
	src := t.TempDir()
	pd := filepath.Join(src, "personas")
	os.MkdirAll(filepath.Join(pd, "rin"), 0o755)
	os.WriteFile(filepath.Join(pd, "rin", "persona.md"), []byte(personaText), 0o644)
	os.WriteFile(filepath.Join(pd, "rin", "voice.json"), []byte(`{"median_words":[2,9]}`), 0o644)
	h := home.Home{Dir: src}
	h.Write(home.Rules, "20-calm", "Stay calm.")
	h.Write(home.Skills, "tea", "---\ndescription: make tea\n---\nBoil water.")
	var buf bytes.Buffer
	if err := Export(pd, h, "rin", []string{"tea"}, []string{"20-calm"}, &buf); err != nil {
		t.Fatal(err)
	}
	arc := filepath.Join(t.TempDir(), "rin.tar.gz")
	os.WriteFile(arc, buf.Bytes(), 0o644)

	p, cleanup, err := Fetch(arc)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if p.ID != "rin" || p.Voice == "" || len(p.Skills) != 1 || len(p.Rules) != 1 {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(p.Summary(), "will follow") {
		t.Fatal("the summary must say this is text she obeys")
	}

	dstHome := t.TempDir()
	dst := home.Home{Dir: dstHome}
	dpd := filepath.Join(dstHome, "personas")
	if err := p.Install(dpd, dst, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dpd, "rin", "voice.json")); !strings.Contains(string(b), "median_words") {
		t.Fatal("voice targets not installed")
	}
	if r, _ := dst.Read(home.Rules, "20-calm"); r != "Stay calm." {
		t.Fatal("rule not installed")
	}
	// a second install refuses, then --force replaces and keeps the old one
	if err := p.Install(dpd, dst, false); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("overwrote without force: %v", err)
	}
	os.WriteFile(filepath.Join(dpd, "rin", "persona.md"), []byte("---\nname: Rin\n---\nMy edited version."), 0o644)
	if err := p.Install(dpd, dst, true); err != nil {
		t.Fatal(err)
	}
	bs, _ := filepath.Glob(filepath.Join(dstHome, ".backup", "*pack-rin-persona.md"))
	if len(bs) != 1 {
		t.Fatalf("the edited version was not backed up: %v", bs)
	}
	if b, _ := os.ReadFile(bs[0]); !strings.Contains(string(b), "My edited version") {
		t.Fatal("backup is not the edited file")
	}
}

func TestFetchSaysWhatWasWrong(t *testing.T) {
	if _, _, err := Fetch("not a thing"); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("%v", err)
	}
	if _, _, err := Fetch("http://insecure.example/x"); err == nil {
		t.Fatal("plain http accepted")
	}
}
