package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/sdk"
)

// owner makes an owner person and returns a tool env for one of their messages.
func ownerEnv(t *testing.T, e *Engine, msgID string) *sdk.CallEnv {
	t.Helper()
	p, _ := e.Mem.Resolve(context.Background(), "discord", "boss", "boss")
	e.Mem.ClaimOwner(context.Background(), p.ID)
	return &sdk.CallEnv{Transport: "discord", MessageID: msgID, Speaker: sdk.Identity{ID: p.ID, Name: "boss"}}
}

func call(t *testing.T, e *Engine, name string, args any, env *sdk.CallEnv) string {
	t.Helper()
	b, _ := json.Marshal(args)
	tool, ok := e.Tools.Get(name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	out, err := tool.Call(context.Background(), b, env)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const newPersona = "---\nname: Rin\n---\nYou are Rin. Calm, precise, a little tired. You answer in one or two lines."

func TestWorkshopIsOwnerOnly(t *testing.T) {
	e, _, _ := setup(t)
	p, _ := e.Mem.Resolve(context.Background(), "discord", "stranger", "stranger")
	env := &sdk.CallEnv{Transport: "discord", MessageID: "1", Speaker: sdk.Identity{ID: p.ID}}
	for _, n := range []string{"propose_change", "apply_change"} {
		if out := call(t, e, n, map[string]any{"kind": "rule", "name": "x", "text": "y", "id": "c1"}, env); !strings.Contains(out, "only your owner") {
			t.Fatalf("%s let a stranger in: %q", n, out)
		}
	}
}

func TestAPersonaChangeNeedsAnotherMessageAndKeepsABackup(t *testing.T) {
	e, _, _ := setup(t, say("hey."), say("spaces. fight me."), say("sorry, that sounds heavy."))
	lib := e.personaLib().Dir
	old, _ := os.ReadFile(filepath.Join(lib, "maki", "persona.md"))
	out := call(t, e, "propose_change", map[string]any{"kind": "persona", "name": "maki", "text": newPersona}, ownerEnv(t, e, "m1"))
	if !strings.Contains(out, "draft c1") || !strings.Contains(out, "How it answers") || !strings.Contains(out, "spaces. fight me.") || !strings.Contains(out, "- You are Maki.") || !strings.Contains(out, "+ You are Rin.") {
		t.Fatalf("proposal lacks the diff or the samples:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "maki", "persona.md")); string(b) != string(old) {
		t.Fatal("a draft changed the file")
	}
	// the same message cannot both ask and approve
	if out := call(t, e, "apply_change", map[string]any{"id": "c1"}, ownerEnv(t, e, "m1")); !strings.Contains(out, "not in the same message") {
		t.Fatalf("applied in the same turn: %q", out)
	}
	if out := call(t, e, "apply_change", map[string]any{"id": "c1", "activate": true}, ownerEnv(t, e, "m2")); !strings.Contains(out, "done") {
		t.Fatalf("%q", out)
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "maki", "persona.md")); string(b) != newPersona {
		t.Fatal("persona not written")
	}
	if e.Cfg.Get().Persona.Active != "maki" { // id was maki: unchanged but must not error
		t.Fatal("active persona changed unexpectedly")
	}
	files, _ := filepath.Glob(filepath.Join(e.Home().Dir, ".backup", "*persona-maki.md"))
	if len(files) != 1 {
		t.Fatalf("no backup of the old character: %v", files)
	}
	if b, _ := os.ReadFile(files[0]); string(b) != string(old) {
		t.Fatal("backup is not the old content")
	}
	if out := call(t, e, "apply_change", map[string]any{"id": "c1"}, ownerEnv(t, e, "m3")); !strings.Contains(out, "no such draft") {
		t.Fatal("a draft applied twice")
	}
}

func TestSkillsAndRulesAreWrittenThroughTheHomeValidation(t *testing.T) {
	e, _, _ := setup(t)
	env := func(id string) *sdk.CallEnv { return ownerEnv(t, e, id) }
	if out := call(t, e, "propose_change", map[string]any{"kind": "skill", "name": "../etc", "text": "x", "description": "d"}, env("1")); !strings.Contains(out, "names use") {
		t.Fatalf("path in a name accepted: %q", out)
	}
	if out := call(t, e, "propose_change", map[string]any{"kind": "skill", "name": "manga-recs", "text": "Ask what they loved first."}, env("1")); !strings.Contains(out, "one-line description") {
		t.Fatalf("a skill without a description was drafted: %q", out)
	}
	call(t, e, "propose_change", map[string]any{"kind": "skill", "name": "manga-recs", "text": "Ask what they loved first.", "description": "recommend manga like a friend"}, env("1"))
	call(t, e, "propose_change", map[string]any{"kind": "rule", "name": "20-brevity", "text": "Never write more than three lines."}, env("1"))
	call(t, e, "apply_change", map[string]any{"id": "c1"}, env("2"))
	call(t, e, "apply_change", map[string]any{"id": "c2"}, env("2"))
	sk := e.Home().SkillList()
	found := false
	for _, s := range sk {
		found = found || (s.Name == "manga-recs" && s.Description == "recommend manga like a friend")
	}
	if !found {
		t.Fatalf("skill not listed: %+v", sk)
	}
	if !strings.Contains(e.Home().Directives("", ""), "three lines") {
		t.Fatal("rule not in her directives")
	}
}

func TestASettingChangeGoesThroughTheTunableGate(t *testing.T) {
	e, _, _ := setup(t)
	e.Tunable = func(path string, v any) (any, error) {
		if path != "behavior.response.chances.interesting_home" {
			return nil, errors.New(path + " is not something she can change from chat")
		}
		return v, nil
	}
	env := func(id string) *sdk.CallEnv { return ownerEnv(t, e, id) }
	if out := call(t, e, "propose_change", map[string]any{"kind": "setting", "name": "web_ui.token", "value": `"x"`}, env("1")); strings.Contains(out, "draft c") {
		t.Fatalf("the gate was bypassed: %s", out)
	}
	e.Tunable = nil
	if out := call(t, e, "propose_change", map[string]any{"kind": "setting", "name": "behavior.response.chances.interesting_home", "value": "0.1"}, env("1")); strings.Contains(out, "draft c") {
		t.Fatalf("with no gate wired, settings must be refused: %s", out)
	}
	e.Tunable = func(path string, v any) (any, error) { return v, nil }
	out := call(t, e, "propose_change", map[string]any{"kind": "setting", "name": "behavior.response.chances.interesting_home", "value": "0.1"}, env("1"))
	if !strings.Contains(out, "draft c1") {
		t.Fatalf("a legitimate dial was refused: %s", out)
	}
	call(t, e, "apply_change", map[string]any{"id": "c1"}, env("2"))
	if got := e.Cfg.Get().Behavior.Response.Chances.Interesting; got != 0.1 {
		t.Fatalf("dial not applied: %v", got)
	}
}

func TestEveryDraftIsLoggedInTheFeed(t *testing.T) {
	e, _, _ := setup(t)
	call(t, e, "propose_change", map[string]any{"kind": "rule", "name": "30-x", "text": "Be kind."}, ownerEnv(t, e, "1"))
	call(t, e, "apply_change", map[string]any{"id": "c1"}, ownerEnv(t, e, "2"))
	found := false
	for _, ev := range e.Ev.Since(0) {
		found = found || (ev.Type == "system" && strings.Contains(ev.Why, "changed her own setup"))
	}
	if !found {
		t.Fatal("a self-change left no trace in the live feed")
	}
}

func TestADraftCanBeDiscarded(t *testing.T) {
	e, _, _ := setup(t)
	call(t, e, "propose_change", map[string]any{"kind": "rule", "name": "40-x", "text": "Be loud."}, ownerEnv(t, e, "1"))
	if out := call(t, e, "apply_change", map[string]any{"id": "c1", "discard": true}, ownerEnv(t, e, "2")); out != "discarded" {
		t.Fatal(out)
	}
	if out := call(t, e, "apply_change", map[string]any{"id": "c1"}, ownerEnv(t, e, "3")); !strings.Contains(out, "no such draft") {
		t.Fatal(out)
	}
}

func TestPendingDraftsAreRememberedAcrossMessagesAndReplacedWhenRedrafted(t *testing.T) {
	e, _, sc := setup(t, say("want it?"))
	ctx := context.Background()
	own := ownerEnv(t, e, "m1")
	call(t, e, "propose_change", map[string]any{"kind": "rule", "name": "50-brief", "text": "Two lines."}, own)
	call(t, e, "propose_change", map[string]any{"kind": "rule", "name": "50-brief", "text": "Two lines, max."}, ownerEnv(t, e, "m1"))
	if n := len(e.work.items); n != 1 {
		t.Fatalf("redrafting the same thing left %d drafts", n)
	}
	// the owner's next message: she must be told what is waiting
	p, _, _ := e.Mem.Person(ctx, own.Speaker.ID)
	if note := e.pendingNote(ctx, p.ID, "m2"); !strings.Contains(note, "c2") || !strings.Contains(note, "apply_change") {
		t.Fatalf("the next turn does not mention the waiting draft: %q", note)
	}
	if note := e.pendingNote(ctx, p.ID, "m1"); note != "" {
		t.Fatal("the message that made the draft must not be told to apply it")
	}
	other, _ := e.Mem.Resolve(ctx, "discord", "someone", "someone")
	if note := e.pendingNote(ctx, other.ID, "m2"); note != "" {
		t.Fatal("a non-owner was told about the owner's drafts")
	}
	_ = sc
}
