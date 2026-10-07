// Package pack shares a character as a folder: persona.md, optional voice
// targets and eval inputs, and optionally the skills and rules it needs. A pack
// is only data, so installing one runs nothing, but it is text she will
// follow, so installing shows what is inside and asks first.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/persona"
)

const (
	maxFile  = 64 << 10
	maxTotal = 512 << 10
	maxFiles = 40
)

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

// Pack is a validated pack on disk, not yet installed.
type Pack struct {
	Root    string
	ID      string
	Persona string
	Voice   string // voice.json, "" if none
	Eval    string // eval.txt, "" if none
	Skills  map[string]string
	Rules   map[string]string
}

// Summary is what the person reads before saying yes.
func (p *Pack) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "character %q (%d lines of persona.md)\n", p.ID, strings.Count(p.Persona, "\n")+1)
	if p.Voice != "" {
		b.WriteString("  with voice targets (voice.json)\n")
	}
	if p.Eval != "" {
		b.WriteString("  with test inputs (eval.txt)\n")
	}
	for _, n := range sortedKeys(p.Skills) {
		fmt.Fprintf(&b, "skill %q (%d lines)\n", n, strings.Count(p.Skills[n], "\n")+1)
	}
	for _, n := range sortedKeys(p.Rules) {
		fmt.Fprintf(&b, "house rule %q: %s\n", n, firstLine(p.Rules[n]))
	}
	b.WriteString("All of this is text she will follow. Read it before you accept: " + p.Root)
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 100 {
		s = s[:100] + "…"
	}
	return s
}

func sortedKeys(m map[string]string) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}

// Fetch makes a Pack from a directory, a .tar.gz, or a git address, and
// returns a cleanup func for any temporary files.
func Fetch(src string) (*Pack, func(), error) {
	noop := func() {}
	st, err := os.Stat(src)
	switch {
	case err == nil && st.IsDir():
		p, err := Inspect(src)
		return p, noop, err
	case err == nil && (strings.HasSuffix(src, ".tar.gz") || strings.HasSuffix(src, ".tgz")):
		tmp, err := os.MkdirTemp("", "mak1zu-pack-*")
		if err != nil {
			return nil, noop, err
		}
		clean := func() { os.RemoveAll(tmp) }
		f, err := os.Open(src)
		if err != nil {
			clean()
			return nil, noop, err
		}
		defer f.Close()
		if err := Untar(f, tmp); err != nil {
			clean()
			return nil, noop, err
		}
		p, err := Inspect(tmp)
		if err != nil {
			clean()
			return nil, noop, err
		}
		return p, clean, nil
	case err == nil:
		return nil, noop, fmt.Errorf("%s is a file; give me a folder, a .tar.gz or a git address", src)
	}
	url := src
	if regexp.MustCompile(`^[\w.-]+/[\w.-]+$`).MatchString(src) {
		url = "https://github.com/" + src
	}
	if !strings.HasPrefix(url, "https://") {
		return nil, noop, fmt.Errorf("%q is not a folder, a .tar.gz or an https git address (user/repo works for GitHub)", src)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, noop, errors.New("installing from a git address needs git; or download the repository and give me the folder")
	}
	tmp, err := os.MkdirTemp("", "mak1zu-pack-*")
	if err != nil {
		return nil, noop, err
	}
	clean := func() { os.RemoveAll(tmp) }
	cmd := exec.Command(git, "clone", "--depth", "1", "--quiet", "--no-tags", url, filepath.Join(tmp, "repo"))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	done := make(chan error, 1)
	go func() { out, e := cmd.CombinedOutput(); _ = out; done <- e }()
	select {
	case err := <-done:
		if err != nil {
			clean()
			return nil, noop, fmt.Errorf("git could not fetch %s", url)
		}
	case <-time.After(90 * time.Second):
		_ = cmd.Process.Kill()
		clean()
		return nil, noop, errors.New("git took too long")
	}
	_ = os.RemoveAll(filepath.Join(tmp, "repo", ".git"))
	p, err := Inspect(filepath.Join(tmp, "repo"))
	if err != nil {
		clean()
		return nil, noop, err
	}
	return p, clean, nil
}

// Inspect validates a folder as a pack. The persona may sit in the folder
// itself, in personas/<id>/, or in the folder's only subfolder.
func Inspect(root string) (*Pack, error) {
	dir, id, err := findPersona(root)
	if err != nil {
		return nil, err
	}
	total, files := 0, 0
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link; packs may not contain links", rel(root, path))
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", rel(root, path))
		}
		fi, _ := d.Info()
		files++
		total += int(fi.Size())
		if fi.Size() > maxFile {
			return fmt.Errorf("%s is larger than %d KB", rel(root, path), maxFile>>10)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if files > maxFiles || total > maxTotal {
		return nil, fmt.Errorf("too large for a pack (%d files, %d KB; limit %d files, %d KB)", files, total>>10, maxFiles, maxTotal>>10)
	}
	p := &Pack{Root: root, ID: id, Skills: map[string]string{}, Rules: map[string]string{}}
	read := func(path string) string { b, _ := os.ReadFile(path); return string(b) }
	p.Persona = read(filepath.Join(dir, "persona.md"))
	if _, err := persona.Parse(id, p.Persona); err != nil {
		return nil, fmt.Errorf("persona.md: %w", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "voice.json")); err == nil {
		p.Voice = string(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "eval.txt")); err == nil {
		p.Eval = string(b)
	}
	// skills and rules may ship beside the persona or in the pack root
	for _, base := range uniq(dir, root) {
		if es, err := os.ReadDir(filepath.Join(base, "skills")); err == nil {
			for _, e := range es {
				if !e.IsDir() || !nameRe.MatchString(e.Name()) {
					return nil, fmt.Errorf("skills/%s: names use a-z, 0-9, - and _", e.Name())
				}
				b, err := os.ReadFile(filepath.Join(base, "skills", e.Name(), "SKILL.md"))
				if err != nil {
					return nil, fmt.Errorf("skills/%s needs a SKILL.md", e.Name())
				}
				if !strings.Contains(string(b), "description:") {
					return nil, fmt.Errorf("skills/%s/SKILL.md needs a description: line saying when to use it", e.Name())
				}
				p.Skills[e.Name()] = string(b)
			}
		}
		if es, err := os.ReadDir(filepath.Join(base, "rules")); err == nil {
			for _, e := range es {
				n := strings.TrimSuffix(e.Name(), ".md")
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || !nameRe.MatchString(n) {
					return nil, fmt.Errorf("rules/%s: rules are NAME.md with a-z, 0-9, - and _", e.Name())
				}
				b, _ := os.ReadFile(filepath.Join(base, "rules", e.Name()))
				p.Rules[n] = string(b)
			}
		}
	}
	return p, nil
}

func uniq(a, b string) []string {
	if a == b {
		return []string{a}
	}
	return []string{a, b}
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}

func findPersona(root string) (dir, id string, err error) {
	idOf := func(d string) string { return filepath.Base(d) }
	try := func(d string) bool { _, e := os.Stat(filepath.Join(d, "persona.md")); return e == nil }
	switch {
	case try(root):
		id = idOf(root)
		if strings.HasPrefix(id, "mak1zu-pack-") || id == "repo" || id == "." || id == "" || !nameRe.MatchString(id) {
			// the folder name is a temp or arbitrary name: use the persona's own name
			b, _ := os.ReadFile(filepath.Join(root, "persona.md"))
			pa, perr := persona.Parse("x", string(b))
			if perr != nil {
				return "", "", fmt.Errorf("persona.md: %w", perr)
			}
			id = slug(pa.Name)
		}
		return root, id, nil
	}
	var cands []string
	if es, e := os.ReadDir(filepath.Join(root, "personas")); e == nil {
		for _, x := range es {
			if x.IsDir() && try(filepath.Join(root, "personas", x.Name())) {
				cands = append(cands, filepath.Join(root, "personas", x.Name()))
			}
		}
	}
	if len(cands) == 0 {
		if es, e := os.ReadDir(root); e == nil {
			for _, x := range es {
				if x.IsDir() && try(filepath.Join(root, x.Name())) {
					cands = append(cands, filepath.Join(root, x.Name()))
				}
			}
		}
	}
	switch len(cands) {
	case 0:
		return "", "", errors.New("no persona.md found (a pack has persona.md in its folder, or in personas/<id>/)")
	case 1:
		id = filepath.Base(cands[0])
		if !nameRe.MatchString(id) {
			return "", "", fmt.Errorf("persona id %q: use a-z, 0-9, - and _", id)
		}
		return cands[0], id, nil
	}
	return "", "", errors.New("this folder holds several characters; install one folder at a time")
}

func slug(s string) string {
	s = strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "pack"
	}
	if len(s) > 32 {
		s = s[:32]
	}
	return strings.Trim(s, "-")
}

// Conflicts lists what already exists and would be overwritten.
func (p *Pack) Conflicts(personasDir string, h home.Home) []string {
	var c []string
	if _, err := os.Stat(filepath.Join(personasDir, p.ID)); err == nil {
		c = append(c, "character "+p.ID)
	}
	for n := range p.Skills {
		if _, err := h.Read(home.Skills, n); err == nil {
			c = append(c, "skill "+n)
		}
	}
	for n := range p.Rules {
		if _, err := h.Read(home.Rules, n); err == nil {
			c = append(c, "rule "+n)
		}
	}
	sort.Strings(c)
	return c
}

// Install writes the pack into her home. With force, what exists is moved to
// .backup first. Nothing is activated: switching to the new character is a
// separate, deliberate step.
func (p *Pack) Install(personasDir string, h home.Home, force bool) error {
	if c := p.Conflicts(personasDir, h); len(c) > 0 && !force {
		return fmt.Errorf("would overwrite %s; pass --force to replace (the old versions are kept in .makizu/.backup)", strings.Join(c, ", "))
	}
	stamp := time.Now().Format("20060102-150405")
	backup := func(from, name string) {
		b, err := os.ReadFile(from)
		if err != nil {
			return
		}
		dir := filepath.Join(h.Dir, ".backup")
		if os.MkdirAll(dir, 0o700) == nil {
			_ = os.WriteFile(filepath.Join(dir, stamp+"-pack-"+name), b, 0o600)
		}
	}
	backup(filepath.Join(personasDir, p.ID, "persona.md"), p.ID+"-persona.md")
	if err := (persona.Library{Dir: personasDir}).Save(p.ID, p.Persona); err != nil {
		return err
	}
	for name, body := range map[string]string{"voice.json": p.Voice, "eval.txt": p.Eval} {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(personasDir, p.ID, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	for n, body := range p.Skills {
		if old, err := h.Read(home.Skills, n); err == nil {
			_ = os.MkdirAll(filepath.Join(h.Dir, ".backup"), 0o700)
			_ = os.WriteFile(filepath.Join(h.Dir, ".backup", stamp+"-pack-skill-"+n+".md"), []byte(old), 0o600)
		}
		if err := h.Write(home.Skills, n, body); err != nil {
			return err
		}
	}
	for n, body := range p.Rules {
		if old, err := h.Read(home.Rules, n); err == nil {
			_ = os.MkdirAll(filepath.Join(h.Dir, ".backup"), 0o700)
			_ = os.WriteFile(filepath.Join(h.Dir, ".backup", stamp+"-pack-rule-"+n+".md"), []byte(old), 0o600)
		}
		if err := h.Write(home.Rules, n, body); err != nil {
			return err
		}
	}
	return nil
}

// Export writes a character (and optionally the named skills and rules) as a
// .tar.gz that Install and Fetch understand.
func Export(personasDir string, h home.Home, id string, skills, rules []string, w io.Writer) error {
	dir := filepath.Join(personasDir, id)
	if !nameRe.MatchString(id) {
		return fmt.Errorf("bad persona id %q", id)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	add := func(name string, body []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(0, 0)}); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, "persona.md"))
	if err != nil {
		return fmt.Errorf("no persona %q here: %w", id, err)
	}
	if err := add(id+"/persona.md", b); err != nil {
		return err
	}
	for _, f := range []string{"voice.json", "eval.txt"} {
		if b, err := os.ReadFile(filepath.Join(dir, f)); err == nil {
			if err := add(id+"/"+f, b); err != nil {
				return err
			}
		}
	}
	for _, n := range skills {
		t, err := h.Read(home.Skills, n)
		if err != nil {
			return fmt.Errorf("no skill %q: %w", n, err)
		}
		if err := add(id+"/skills/"+n+"/SKILL.md", []byte(t)); err != nil {
			return err
		}
	}
	for _, n := range rules {
		t, err := h.Read(home.Rules, n)
		if err != nil {
			return fmt.Errorf("no rule %q: %w", n, err)
		}
		if err := add(id+"/rules/"+n+".md", []byte(t)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// Untar extracts a .tar.gz into dst, refusing anything that is not a plain
// file or folder inside dst, and anything oversized.
func Untar(r io.Reader, dst string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a gzip file: %w", err)
	}
	tr := tar.NewReader(gz)
	total, n := 0, 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(h.Name)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s would be written outside the pack", h.Name)
		}
		target := filepath.Join(dst, clean)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			n++
			total += int(h.Size)
			if h.Size > maxFile || total > maxTotal || n > maxFiles {
				return errors.New("the archive is larger than a pack may be")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, maxFile+1)); err != nil {
				f.Close()
				return err
			}
			f.Close()
		default:
			return fmt.Errorf("%s: only plain files and folders are allowed in a pack", h.Name)
		}
	}
}
