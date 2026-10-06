// Package home reads and writes the .makizu directory: the rules, skills and
// scoped notes that shape her without touching code. It is her equivalent of a
// project rules folder.
//
//	.makizu/
//	  config.json            settings (secrets stay in .env)
//	  personas/<id>/persona.md
//	  rules/*.md             house rules, always in her prompt (sorted by name)
//	  servers/<guild-id>.md  rules for one server only
//	  channels/<id>.md       rules for one channel only
//	  skills/<name>/SKILL.md know-how she reads on demand (+ references/*.md)
//	  data/                  memory db, telemetry, workspace (never committed)
//
// Everything is plain Markdown, edited by hand or from the panel, and re-read
// every turn, so a change applies without a restart. All names are validated
// and every path is built here, never from model or chat input.
package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxFile     = 32 << 10 // one file written from the panel
	maxRuleFile = 8 << 10  // per rule file in the prompt
	maxRules    = 24 << 10 // all rules together in the prompt
	PageSize    = 8000     // read_skill page, runes
)

var (
	ruleName  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,48}$`)
	skillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)
	idName    = regexp.MustCompile(`^[0-9]{5,25}$`)
	refName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,60}\.md$`)
)

type Home struct{ Dir string }

// Kind is something the panel can edit.
type Kind string

const (
	Rules    Kind = "rules"
	Servers  Kind = "servers"
	Channels Kind = "channels"
	Skills   Kind = "skills"
)

func (k Kind) valid(name string) bool {
	switch k {
	case Rules:
		return ruleName.MatchString(name)
	case Skills:
		return skillName.MatchString(name)
	case Servers, Channels:
		return idName.MatchString(name)
	}
	return false
}

// path maps (kind, name) to its file. The caller must have validated name.
func (h Home) path(k Kind, name string) string {
	if k == Skills {
		return filepath.Join(h.Dir, "skills", name, "SKILL.md")
	}
	return filepath.Join(h.Dir, string(k), name+".md")
}

// readSafe reads a regular file, refusing symlinks, with a size cap.
func readSafe(p string, limit int64) (string, error) {
	st, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if st.Size() > limit {
		return "", fmt.Errorf("file too large (%d bytes)", st.Size())
	}
	b, err := os.ReadFile(p)
	return string(b), err
}

// ---- front matter ----

// Split parses an optional `---` front matter of `key: value` lines.
func Split(text string) (map[string]string, string) {
	meta := map[string]string{}
	if !strings.HasPrefix(text, "---\n") {
		return meta, text
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return meta, text
	}
	for _, l := range strings.Split(rest[:end], "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok {
			meta[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return meta, strings.TrimLeft(rest[end+4:], "\n")
}

// ---- rules ----

// Directives assembles the house rules for one turn: global rules, then the
// server's, then the channel's. A rule file may set `enabled: false`.
func (h Home) Directives(guildID, channelID string) string {
	var b strings.Builder
	add := func(label, p string) {
		txt, err := readSafe(p, maxRuleFile)
		if err != nil {
			return
		}
		meta, body := Split(txt)
		body = strings.TrimSpace(body)
		if body == "" || meta["enabled"] == "false" {
			return
		}
		if b.Len()+len(body) > maxRules {
			return
		}
		fmt.Fprintf(&b, "### %s\n%s\n\n", label, body)
	}
	for _, n := range h.names(Rules) {
		add("rule: "+n, h.path(Rules, n))
	}
	if idName.MatchString(guildID) {
		add("this server", h.path(Servers, guildID))
	}
	if idName.MatchString(channelID) {
		add("this channel", h.path(Channels, channelID))
	}
	return strings.TrimSpace(b.String())
}

func (h Home) names(k Kind) []string {
	ents, _ := os.ReadDir(filepath.Join(h.Dir, string(k)))
	var out []string
	for _, e := range ents {
		n := e.Name()
		switch {
		case k == Skills && e.IsDir() && skillName.MatchString(n):
			out = append(out, n)
		case k != Skills && !e.IsDir() && strings.HasSuffix(n, ".md") && k.valid(strings.TrimSuffix(n, ".md")):
			out = append(out, strings.TrimSuffix(n, ".md"))
		}
	}
	sort.Strings(out)
	return out
}

// ---- skills ----

type Skill struct {
	Name        string
	Description string
}

// SkillList is the catalog shown in the prompt: name and one-line
// description only. The body is read on demand.
func (h Home) SkillList() []Skill {
	var out []Skill
	for _, n := range h.names(Skills) {
		txt, err := readSafe(h.path(Skills, n), maxFile)
		if err != nil {
			continue
		}
		meta, _ := Split(txt)
		if meta["enabled"] == "false" {
			continue
		}
		out = append(out, Skill{Name: n, Description: meta["description"]})
	}
	return out
}

// ReadSkill returns one page of SKILL.md or of references/<file>. name and
// file are validated; nothing else can be reached.
func (h Home) ReadSkill(name, file string, offset int) (string, error) {
	if !skillName.MatchString(name) {
		return "", errors.New("unknown skill")
	}
	p := h.path(Skills, name)
	if file != "" {
		if !refName.MatchString(file) {
			return "", errors.New("bad reference name")
		}
		p = filepath.Join(h.Dir, "skills", name, "references", file)
	}
	txt, err := readSafe(p, maxFile)
	if err != nil {
		return "", errors.New("not found")
	}
	_, body := Split(txt)
	r := []rune(body)
	if offset < 0 || offset > len(r) {
		offset = 0
	}
	end := min(offset+PageSize, len(r))
	out := string(r[offset:end])
	if end < len(r) {
		out += fmt.Sprintf("\n…(more: call read_skill again with offset=%d)", end)
	}
	if file == "" {
		if refs := h.refs(name); len(refs) > 0 {
			out += "\nreferences: " + strings.Join(refs, ", ")
		}
	}
	return out, nil
}

func (h Home) refs(name string) []string {
	ents, _ := os.ReadDir(filepath.Join(h.Dir, "skills", name, "references"))
	var out []string
	for _, e := range ents {
		if !e.IsDir() && refName.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// ---- panel editing ----

func (h Home) List(k Kind) ([]string, error) {
	switch k {
	case Rules, Servers, Channels, Skills:
		return h.names(k), nil
	}
	return nil, errors.New("unknown kind")
}

func (h Home) Read(k Kind, name string) (string, error) {
	if !k.valid(name) {
		return "", errors.New("bad name")
	}
	return readSafe(h.path(k, name), maxFile)
}

func (h Home) Write(k Kind, name, text string) error {
	if !k.valid(name) {
		return errors.New("bad name (rules: a-z0-9_-, skills: a-z0-9-, servers/channels: the numeric ID)")
	}
	if len(text) > maxFile || strings.TrimSpace(text) == "" {
		return fmt.Errorf("content must be 1 to %d bytes", maxFile)
	}
	p := h.path(k, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if st, err := os.Lstat(p); err == nil && !st.Mode().IsRegular() {
		return errors.New("refusing to write over a non-regular file")
	}
	return os.WriteFile(p, []byte(text), 0o644)
}

func (h Home) Delete(k Kind, name string) error {
	if !k.valid(name) {
		return errors.New("bad name")
	}
	if k == Skills {
		return os.RemoveAll(filepath.Join(h.Dir, "skills", name))
	}
	return os.Remove(h.path(k, name))
}
