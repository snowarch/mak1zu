package home

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func defs(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for p, c := range files {
		m[p] = &fstest.MapFile{Data: []byte(c)}
	}
	return m
}

func actions(cs []Change) map[string]Action {
	m := map[string]Action{}
	for _, c := range cs {
		m[c.Path] = c.Action
	}
	return m
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sync(t *testing.T, d fstest.MapFS, dir string, o SyncOptions) map[string]Action {
	t.Helper()
	cs, err := SyncDefaults(d, dir, o)
	if err != nil {
		t.Fatal(err)
	}
	return actions(cs)
}

func TestFreshInstallThenNothingToDo(t *testing.T) {
	dir := t.TempDir()
	d := defs(map[string]string{"rules/a.md": "A1", "personas/x/persona.md": "P1"})
	got := sync(t, d, dir, SyncOptions{})
	if got["rules/a.md"] != Added || got["personas/x/persona.md"] != Added {
		t.Fatalf("%v", got)
	}
	if read(t, filepath.Join(dir, "rules/a.md")) != "A1" {
		t.Fatal("not written")
	}
	for p, a := range sync(t, d, dir, SyncOptions{}) {
		if a != Same {
			t.Errorf("%s: %s on the second run", p, a)
		}
	}
}

func TestUntouchedFilesFollowTheRelease(t *testing.T) {
	dir := t.TempDir()
	sync(t, defs(map[string]string{"rules/a.md": "A1", "rules/b.md": "B1"}), dir, SyncOptions{})
	got := sync(t, defs(map[string]string{"rules/a.md": "A2", "rules/b.md": "B1", "rules/c.md": "C1"}), dir, SyncOptions{})
	if got["rules/a.md"] != Updated || got["rules/b.md"] != Same || got["rules/c.md"] != Added {
		t.Fatalf("%v", got)
	}
	if read(t, filepath.Join(dir, "rules/a.md")) != "A2" {
		t.Fatal("not updated")
	}
}

func TestYourEditsAreNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	sync(t, defs(map[string]string{"rules/a.md": "A1", "rules/b.md": "B1"}), dir, SyncOptions{})
	write(t, filepath.Join(dir, "rules/a.md"), "mine A")
	write(t, filepath.Join(dir, "rules/b.md"), "mine B")

	// the release changed a.md only
	got := sync(t, defs(map[string]string{"rules/a.md": "A2", "rules/b.md": "B1"}), dir, SyncOptions{})
	if got["rules/a.md"] != Conflict {
		t.Fatalf("a: %v", got)
	}
	if got["rules/b.md"] != Customized {
		t.Fatalf("b was edited and the release did not touch it, that is not a conflict: %v", got)
	}
	if read(t, filepath.Join(dir, "rules/a.md")) != "mine A" || read(t, filepath.Join(dir, "rules/b.md")) != "mine B" {
		t.Fatal("an edit was overwritten")
	}
	if read(t, filepath.Join(dir, ".updates/rules/a.md")) != "A2" {
		t.Fatal("new version was not staged for comparison")
	}
	// resolving by hand ends the conflict
	write(t, filepath.Join(dir, "rules/a.md"), "A2")
	if got := sync(t, defs(map[string]string{"rules/a.md": "A2", "rules/b.md": "B1"}), dir, SyncOptions{}); got["rules/a.md"] != Same {
		t.Fatalf("%v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".updates")); err == nil {
		t.Fatal("stale staged copies were not cleared")
	}
}

func TestInstallWithoutAManifestIsTreatedCarefully(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rules/same.md"), "S")
	write(t, filepath.Join(dir, "rules/diff.md"), "who knows")
	got := sync(t, defs(map[string]string{"rules/same.md": "S", "rules/diff.md": "NEW", "rules/new.md": "N"}), dir, SyncOptions{})
	if got["rules/same.md"] != Same || got["rules/diff.md"] != Conflict || got["rules/new.md"] != Added {
		t.Fatalf("%v", got)
	}
	if read(t, filepath.Join(dir, "rules/diff.md")) != "who knows" {
		t.Fatal("overwrote a file with unknown history")
	}
	// the identical file is now tracked: a later release can update it safely
	if got := sync(t, defs(map[string]string{"rules/same.md": "S2"}), dir, SyncOptions{}); got["rules/same.md"] != Updated {
		t.Fatalf("%v", got)
	}
}

func TestADeletedFileStaysDeletedUntilForced(t *testing.T) {
	dir := t.TempDir()
	d := defs(map[string]string{"rules/a.md": "A1"})
	sync(t, d, dir, SyncOptions{})
	os.Remove(filepath.Join(dir, "rules/a.md"))
	if got := sync(t, d, dir, SyncOptions{}); got["rules/a.md"] != Removed {
		t.Fatalf("%v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules/a.md")); err == nil {
		t.Fatal("brought a deleted file back")
	}
	if got := sync(t, d, dir, SyncOptions{Force: true}); got["rules/a.md"] != Added {
		t.Fatalf("%v", got)
	}
}

func TestForceReplacesButKeepsTheOldCopy(t *testing.T) {
	dir := t.TempDir()
	sync(t, defs(map[string]string{"rules/a.md": "A1"}), dir, SyncOptions{})
	write(t, filepath.Join(dir, "rules/a.md"), "mine")
	got := sync(t, defs(map[string]string{"rules/a.md": "A2"}), dir, SyncOptions{Force: true})
	if got["rules/a.md"] != Replaced || read(t, filepath.Join(dir, "rules/a.md")) != "A2" {
		t.Fatalf("%v", got)
	}
	var kept string
	filepath.WalkDir(filepath.Join(dir, backupDir), func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			kept = read(t, p)
		}
		return nil
	})
	if kept != "mine" {
		t.Fatalf("backup holds %q, want the old copy", kept)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	sync(t, defs(map[string]string{"rules/a.md": "A1"}), dir, SyncOptions{})
	write(t, filepath.Join(dir, "rules/a.md"), "mine")
	before := read(t, filepath.Join(dir, ManifestName))
	got := sync(t, defs(map[string]string{"rules/a.md": "A2", "rules/n.md": "N"}), dir, SyncOptions{DryRun: true, Force: true})
	if got["rules/a.md"] != Replaced || got["rules/n.md"] != Added {
		t.Fatalf("%v", got)
	}
	if read(t, filepath.Join(dir, "rules/a.md")) != "mine" || read(t, filepath.Join(dir, ManifestName)) != before {
		t.Fatal("dry run changed the install")
	}
	for _, p := range []string{"rules/n.md", ".backup", ".updates"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			t.Errorf("dry run created %s", p)
		}
	}
}

func TestInitOnlyNeverTouchesWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rules/a.md"), "mine")
	got := sync(t, defs(map[string]string{"rules/a.md": "A", "rules/b.md": "B"}), dir, SyncOptions{InitOnly: true})
	if got["rules/a.md"] != Customized || got["rules/b.md"] != Added {
		t.Fatalf("%v", got)
	}
	if read(t, filepath.Join(dir, "rules/a.md")) != "mine" {
		t.Fatal("init overwrote an existing file")
	}
	if _, err := os.Stat(filepath.Join(dir, ".updates")); err == nil {
		t.Fatal("init staged files")
	}
}

func TestNeverWritesThroughASymlink(t *testing.T) {
	dir, outside := t.TempDir(), filepath.Join(t.TempDir(), "secret.txt")
	write(t, outside, "do not touch")
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "rules/a.md")); err != nil {
		t.Skip("no symlinks here")
	}
	for _, o := range []SyncOptions{{}, {Force: true}} {
		got := sync(t, defs(map[string]string{"rules/a.md": "A"}), dir, o)
		if got["rules/a.md"] != Conflict {
			t.Fatalf("%+v: %v", o, got)
		}
		if read(t, outside) != "do not touch" {
			t.Fatalf("%+v: wrote through a symlink", o)
		}
	}
}

func TestUserFilesOutsideTheDefaultsAreLeftAlone(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rules/mine.md"), "mine")
	write(t, filepath.Join(dir, "channels/123.md"), "channel note")
	sync(t, defs(map[string]string{"rules/a.md": "A"}), dir, SyncOptions{})
	if read(t, filepath.Join(dir, "rules/mine.md")) != "mine" || read(t, filepath.Join(dir, "channels/123.md")) != "channel note" {
		t.Fatal("touched a file that is not a shipped default")
	}
}

func TestPendingCounts(t *testing.T) {
	a, d := Pending([]Change{{Action: Added}, {Action: Updated}, {Action: Same}, {Action: Customized}, {Action: Conflict}, {Action: Removed}})
	if a != 2 || d != 1 {
		t.Fatal(a, d)
	}
}
