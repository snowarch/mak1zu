package home

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Defaults are the personas, rules and skills a release ships. `mak1zu init`
// copies them once, which leaves an old install frozen. SyncDefaults brings an
// install up to date without ever overwriting what the person wrote: it keeps a
// manifest of the hash of every default it installed, so "you edited this" and
// "the project changed this" can be told apart.

// ManifestName is the file, inside the .makizu directory, that remembers what
// was installed.
const ManifestName = ".defaults.json"

const (
	updatesDir = ".updates" // the new version of a file you changed, to compare
	backupDir  = ".backup"  // what --force replaced
)

// Action is what happened (or would happen) to one default file.
type Action string

const (
	Added      Action = "added"      // new in this release, or first install
	Updated    Action = "updated"    // you never touched it, so it moved to the new version
	Same       Action = "same"       // already identical
	Customized Action = "customized" // you changed it and the release did not: left alone
	Conflict   Action = "conflict"   // you changed it and the release did too: left alone, new copy staged
	Removed    Action = "removed"    // you deleted it earlier: left deleted
	Replaced   Action = "replaced"   // --force: overwritten, old copy kept in .backup
)

// Change is one file's outcome. Path is relative to the .makizu directory.
type Change struct {
	Path   string
	Action Action
	Note   string
}

// SyncOptions tunes a sync.
type SyncOptions struct {
	// DryRun reports what would happen and writes nothing.
	DryRun bool
	// Force overwrites conflicts and restores deleted files, keeping the old
	// copy under .backup/<time>/.
	Force bool
	// InitOnly adds missing files and never touches or stages an existing one.
	// It is what `mak1zu init` uses on a new folder.
	InitOnly bool
	// Version is recorded in the manifest, for people reading it.
	Version string
}

type manifest struct {
	Version string            `json:"version,omitempty"`
	Files   map[string]string `json:"files"`
}

// SyncDefaults compares the shipped defaults (an fs rooted at the contents of
// .makizu, for example fs.Sub(embedded, ".makizu")) with the install in dir.
func SyncDefaults(defaults fs.FS, dir string, opt SyncOptions) ([]Change, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil && !opt.DryRun {
		return nil, err
	}
	man := readManifest(dir)
	var paths []string
	err := fs.WalkDir(defaults, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && p != ManifestName {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	stamp := time.Now().UTC().Format("20060102-150405")
	if !opt.DryRun && !opt.InitOnly {
		_ = os.RemoveAll(filepath.Join(dir, updatesDir)) // stale comparisons from a past run
	}
	var out []Change
	for _, p := range paths {
		want, err := fs.ReadFile(defaults, p)
		if err != nil {
			return nil, err
		}
		wantHash := hashOf(want)
		dst := filepath.Join(dir, filepath.FromSlash(p))
		have, state := readInstalled(dst)
		base, hadBase := man.Files[p]

		change := Change{Path: p}
		write := func(content []byte, a Action, note string) error {
			change.Action, change.Note = a, note
			if opt.DryRun {
				return nil
			}
			if err := writeFileAtomic(dst, content); err != nil {
				return err
			}
			man.Files[p] = wantHash
			return nil
		}

		switch {
		case state == stateMissing && hadBase && !opt.Force && !opt.InitOnly:
			change.Action, change.Note = Removed, "you deleted it; --force brings it back"
		case state == stateMissing:
			err = write(want, Added, "")
		case state == stateOther:
			// a symlink or a directory where a file belongs: never write through it
			change.Action, change.Note = Conflict, "not a regular file; left alone"
		case hashOf(have) == wantHash:
			change.Action = Same
			if !opt.DryRun {
				man.Files[p] = wantHash
			}
		case opt.InitOnly:
			change.Action, change.Note = Customized, "already there; kept"
		case hadBase && hashOf(have) == base:
			err = write(want, Updated, "")
		case hadBase && base == wantHash:
			change.Action, change.Note = Customized, "yours; the release did not change it"
		case opt.Force:
			if !opt.DryRun {
				if err = backup(dir, stamp, p, have); err != nil {
					return nil, err
				}
			}
			err = write(want, Replaced, "old copy in "+filepath.ToSlash(filepath.Join(backupDir, stamp, p)))
		default:
			note := "you changed it and so did the release"
			if !hadBase {
				note = "differs from the release and there is no record of what you started from"
			}
			change.Action, change.Note = Conflict, note
			if !opt.DryRun {
				if err = writeFileAtomic(filepath.Join(dir, updatesDir, filepath.FromSlash(p)), want); err != nil {
					return nil, err
				}
				change.Note += "; new version in " + filepath.ToSlash(filepath.Join(updatesDir, p))
			}
		}
		if err != nil {
			return nil, err
		}
		out = append(out, change)
	}
	if !opt.DryRun {
		man.Version = opt.Version
		if err := writeManifest(dir, man); err != nil {
			return nil, err
		}
	}
	return out, nil
}

type fileState int

const (
	stateMissing fileState = iota
	stateFile
	stateOther
)

func readInstalled(p string) ([]byte, fileState) {
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, stateMissing
	}
	if !fi.Mode().IsRegular() {
		return nil, stateOther
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, stateOther
	}
	return b, stateFile
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func readManifest(dir string) manifest {
	m := manifest{Files: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return m
	}
	if json.Unmarshal(b, &m) != nil || m.Files == nil {
		return manifest{Files: map[string]string{}}
	}
	return m
}

func writeManifest(dir string, m manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, ManifestName), append(b, '\n'))
}

func backup(dir, stamp, rel string, content []byte) error {
	return writeFileAtomic(filepath.Join(dir, backupDir, stamp, filepath.FromSlash(rel)), content)
}

// writeFileAtomic writes beside the target and renames, so a crash never leaves
// half a persona behind.
func writeFileAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Pending counts the changes a sync would act on or needs a decision about:
// everything except files already current, yours by choice, or deleted by you.
func Pending(changes []Change) (apply, decide int) {
	for _, c := range changes {
		switch c.Action {
		case Added, Updated:
			apply++
		case Conflict:
			decide++
		}
	}
	return
}
