package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/tools"
)

// The workshop: her owner can ask her to change how she works ("give yourself
// a calmer voice", "write a skill for recommending manga", "talk less in
// busy rooms") and she does it, on request and never by herself. The surface
// is deliberately small and typed: a character file, a skill, a house rule,
// or one of the dials the panel documents. Never her guard, her policy, keys,
// rooms, providers or the panel. A change is first a proposal with a diff; it
// only lands in a later message than the one that asked, only for the owner,
// with the old file saved to .backup/ and the change shown in the live feed.

const (
	proposalTTL  = 30 * time.Minute
	maxProposals = 5
)

type proposal struct {
	ID      string
	Kind    string // persona | skill | rule | setting
	Name    string // persona id, skill name, rule name, or setting path
	Old     string
	New     string
	Value   any // for settings
	MsgID   string
	Created time.Time
	Summary string
}

type workshop struct {
	mu    sync.Mutex
	seq   int
	items map[string]*proposal
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,40}$`)

func (e *Engine) registerWorkshop() {
	e.Tools.Add(sdk.ToolFunc{
		S: sdk.ToolSpec{Name: "propose_change", Description: "Draft a change to how you work, only when your owner asks: persona, skill, rule or setting. Nothing changes until apply_change.",
			Schema: tools.Schema([]string{"kind", "name"}, map[string][2]string{
				"kind":        {"string", "persona, skill, rule or setting"},
				"name":        {"string", "id, name or setting path"},
				"text":        {"string", "the whole new file or body"},
				"description": {"string", "skills: when to use it"},
				"value":       {"string", "settings: JSON value"},
			})},
		F: e.proposeChange,
	})
	e.Tools.Add(sdk.ToolFunc{
		S: sdk.ToolSpec{Name: "apply_change", Description: "Apply a draft after your owner said yes in a later message. discard drops it instead.",
			Schema: tools.Schema([]string{"id"}, map[string][2]string{"id": {"string", "draft id"}, "activate": {"boolean", "persona: make it active"}, "discard": {"boolean", "drop the draft"}})},
		F: e.applyChange,
	})
}

func (e *Engine) requireOwner(ctx context.Context, env *sdk.CallEnv) error {
	p, ok, _ := e.Mem.Person(ctx, env.Speaker.ID)
	if !ok || !p.IsOwner() {
		return errors.New("only your owner can change how you work")
	}
	return nil
}

func (e *Engine) proposeChange(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
	if err := e.requireOwner(ctx, env); err != nil {
		return err.Error(), nil
	}
	var a struct{ Kind, Name, Text, Description, Value string }
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	p := &proposal{Kind: a.Kind, Name: a.Name, MsgID: env.MessageID, Created: time.Now()}
	if env.MessageID == "" {
		return "cannot tell which message this is, so the confirmation rule cannot hold; not drafted", nil
	}
	var samples string
	switch a.Kind {
	case "persona":
		if !nameRe.MatchString(a.Name) {
			return "persona ids use a-z, 0-9 and -", nil
		}
		pa, err := persona.Parse(a.Name, a.Text)
		if err != nil {
			return "that is not a valid character file: " + err.Error(), nil
		}
		p.New = a.Text
		if b, err := os.ReadFile(filepath.Join(e.personaLib().Dir, a.Name, "persona.md")); err == nil {
			p.Old = string(b)
		}
		p.Summary = map[bool]string{true: "rewrite her character file ", false: "a new character file "}[p.Old != ""] + a.Name
		samples = e.samples(ctx, pa)
	case "skill", "rule":
		kind := home.Rules
		if a.Kind == "skill" {
			kind = home.Skills
			if strings.TrimSpace(a.Description) == "" {
				return "a skill needs a one-line description saying when to use it", nil
			}
			a.Text = "---\ndescription: " + strings.Join(strings.Fields(a.Description), " ") + "\n---\n" + strings.TrimSpace(a.Text) + "\n"
		}
		if !nameRe.MatchString(a.Name) {
			return "names use a-z, 0-9, - and _", nil
		}
		if strings.TrimSpace(a.Text) == "" {
			return "the text is empty", nil
		}
		p.New = a.Text
		if old, err := e.Home().Read(kind, a.Name); err == nil {
			p.Old = old
		}
		p.Summary = map[bool]string{true: "rewrite the " + a.Kind + " ", false: "write a new " + a.Kind + " "}[p.Old != ""] + a.Name
	case "setting":
		if e.Tunable == nil {
			return "changing dials from chat is not wired in this build", nil
		}
		var v any
		if err := json.Unmarshal([]byte(a.Value), &v); err != nil {
			return "value must be JSON: true, 0.5, \"auto\"", nil
		}
		nv, err := e.Tunable(a.Name, v)
		if err != nil {
			return err.Error(), nil
		}
		old := "(its default)"
		if cur, ok := configValue(e.Cfg.Get(), a.Name); ok {
			old = fmt.Sprint(cur)
		}
		p.Old, p.New, p.Value = old, fmt.Sprint(nv), nv
		p.Summary = fmt.Sprintf("set %s from %s to %s", a.Name, old, p.New)
	default:
		return "kind is persona, skill, rule or setting", nil
	}

	w := &e.work
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.items == nil {
		w.items = map[string]*proposal{}
	}
	for id, it := range w.items {
		if time.Since(it.Created) > proposalTTL {
			delete(w.items, id)
		}
	}
	if len(w.items) >= maxProposals {
		return "there are already several drafts waiting; apply or discard some first", nil
	}
	w.seq++
	p.ID = fmt.Sprintf("c%d", w.seq)
	w.items[p.ID] = p
	return fmt.Sprintf("draft %s: %s\n\n%s%s\nNothing has changed. Tell your owner in your own words what this would do (and, for a character, how the sample replies sound), and ask if they want it. Only after they say yes, in their next message, call apply_change with id %s.",
		p.ID, p.Summary, diffText(p.Old, p.New), samples, p.ID), nil
}

func (e *Engine) applyChange(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
	if err := e.requireOwner(ctx, env); err != nil {
		return err.Error(), nil
	}
	var a struct {
		ID                string
		Activate, Discard bool
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", err
	}
	w := &e.work
	w.mu.Lock()
	p, ok := w.items[a.ID]
	if ok && a.Discard {
		delete(w.items, a.ID)
		w.mu.Unlock()
		return "discarded", nil
	}
	w.mu.Unlock()
	switch {
	case !ok:
		return "no such draft (they expire after half an hour)", nil
	case time.Since(p.Created) > proposalTTL:
		return "that draft expired; draft it again", nil
	case p.MsgID == env.MessageID:
		return "not in the same message as the draft: ask first, apply after they answer", nil
	}
	if err := e.write(p, a.Activate); err != nil {
		return "could not apply it: " + err.Error(), nil
	}
	w.mu.Lock()
	delete(w.items, a.ID)
	w.mu.Unlock()
	e.Log.Info("workshop", "change", p.Summary)
	e.Ev.Emit(events.Event{Type: "system", Why: "she changed her own setup: " + p.Summary, Text: strings.TrimSpace(clip(diffText(p.Old, p.New), 400))})
	return "done: " + p.Summary + ". The old version is saved in .makizu/.backup.", nil
}

func (e *Engine) write(p *proposal, activate bool) error {
	backup := func(label, old string) {
		if old == "" {
			return
		}
		dir := filepath.Join(e.Home().Dir, ".backup")
		if os.MkdirAll(dir, 0o700) == nil {
			_ = os.WriteFile(filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+label), []byte(old), 0o600)
		}
	}
	switch p.Kind {
	case "persona":
		backup("persona-"+p.Name+".md", p.Old)
		if err := e.personaLib().Save(p.Name, p.New); err != nil {
			return err
		}
		if activate {
			return e.Cfg.Patch(map[string]any{"persona.active": p.Name})
		}
	case "skill":
		backup("skill-"+p.Name+".md", p.Old)
		return e.Home().Write(home.Skills, p.Name, p.New)
	case "rule":
		backup("rule-"+p.Name+".md", p.Old)
		return e.Home().Write(home.Rules, p.Name, p.New)
	case "setting":
		return e.Cfg.Patch(map[string]any{p.Name: p.Value})
	}
	return nil
}

// samples asks a character she is about to adopt how it would answer three
// ordinary things, so the owner hears the voice before saying yes.
func (e *Engine) samples(ctx context.Context, pa persona.Persona) string {
	var b strings.Builder
	for _, q := range []string{"hey", "tabs or spaces?", "i had a rough day"} {
		r, err := e.PreviewAs(ctx, pa, "You", []PreviewTurn{{Role: "you", Text: q}})
		if err != nil {
			return ""
		}
		fmt.Fprintf(&b, "  you: %s\n  %s: %s\n", q, strings.ToLower(pa.Name), r.Reply)
	}
	return "\nHow it answers:\n" + b.String()
}

// diffText is a small line diff: enough for an owner to see what changes.
func diffText(old, new string) string {
	a, b := strings.Split(strings.TrimRight(old, "\n"), "\n"), strings.Split(strings.TrimRight(new, "\n"), "\n")
	if old == "" {
		a = nil
	}
	// longest common subsequence over lines
	n, m := len(a), len(b)
	if n*m > 400000 {
		return fmt.Sprintf("(too large to diff line by line: %d lines become %d)\n", n, m)
	}
	l := make([][]int, n+1)
	for i := range l {
		l[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else {
				l[i][j] = max(l[i+1][j], l[i][j+1])
			}
		}
	}
	var out strings.Builder
	add, del := 0, 0
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && a[i] == b[j]:
			i++
			j++
		case j < m && (i == n || l[i][j+1] >= l[i+1][j]):
			fmt.Fprintf(&out, "+ %s\n", clip(b[j], 160))
			add++
			j++
		default:
			fmt.Fprintf(&out, "- %s\n", clip(a[i], 160))
			del++
			i++
		}
	}
	if add+del == 0 {
		return "(no change)\n"
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], fmt.Sprintf("… %d more changed lines", len(lines)-40))
	}
	return fmt.Sprintf("+%d −%d lines\n%s\n", add, del, strings.Join(lines, "\n"))
}

// configValue reads a dotted path out of the config as the panel sees it.
func configValue(c interface{}, path string) (any, bool) {
	b, err := json.Marshal(c)
	if err != nil {
		return nil, false
	}
	var m any
	_ = json.Unmarshal(b, &m)
	for _, k := range strings.Split(path, ".") {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil, false
		}
		if m, ok = mm[k]; !ok {
			return nil, false
		}
	}
	return m, true
}
