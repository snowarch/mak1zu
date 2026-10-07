package distill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/voice"
)

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadsTheCommonExportFormats(t *testing.T) {
	dce := `{"messages":[{"content":"hey","author":{"id":"1","name":"snow","nickname":"Snow F"}},{"content":"hi","author":{"id":"2","name":"bot","isBot":true}},{"content":"yo","author":{"id":"3","name":"kai"}}]}`
	m, err := Load(write(t, "a.json", dce))
	if err != nil || len(m) != 2 || m[0].Author != "Snow F" || m[1].Prev != "hey" {
		t.Fatalf("DiscordChatExporter: %+v %v", m, err)
	}
	pkg := `[{"ID":"1","Contents":"first"},{"ID":"2","Contents":"second"}]`
	if m, err = Load(write(t, "messages.json", pkg)); err != nil || len(m) != 2 || m[0].Text != "first" {
		t.Fatalf("data package: %+v %v", m, err)
	}
	wa := "12/05/2026, 21:14 - Ana: ey\n12/05/2026, 21:15 - Snow: que onda\nsigo aca\n[12/05/26, 9:16:01 PM] Ana: dale"
	if m, err = Load(write(t, "wa.txt", wa)); err != nil || len(m) != 3 || m[1].Author != "Snow" || !strings.Contains(m[1].Text, "sigo aca") || m[1].Prev != "ey" {
		t.Fatalf("whatsapp: %+v %v", m, err)
	}
	if m, err = Load(write(t, "lines.txt", "ana: hola\nsnow: buenas")); err != nil || len(m) != 2 || m[1].Author != "snow" {
		t.Fatalf("name lines: %+v %v", m, err)
	}
	if m, err = Load(write(t, "plain.txt", "one\ntwo\nthree")); err != nil || len(m) != 3 || m[0].Author != "me" {
		t.Fatalf("plain: %+v %v", m, err)
	}
	if _, err = Load(write(t, "x.json", `{"nope":1}`)); err == nil {
		t.Fatal("an unknown JSON must be refused, not guessed")
	}
	if _, err = Load(write(t, "empty.txt", "  ")); err == nil {
		t.Fatal("empty export accepted")
	}
}

func TestSpeakerByNameOrByVolume(t *testing.T) {
	m := []Msg{{Author: "Ana"}, {Author: "Snow"}, {Author: "Snow"}}
	if w, _ := Speaker(m, ""); w != "Snow" {
		t.Fatal(w)
	}
	if w, _ := Speaker(m, "ana"); w != "Ana" {
		t.Fatal(w)
	}
	if _, err := Speaker(m, "nobody"); err == nil || !strings.Contains(err.Error(), "Snow (2)") {
		t.Fatalf("an unknown name should list who is there: %v", err)
	}
}

func TestCleanTakesOutWhatIsNotTheirsToShare(t *testing.T) {
	in := "look https://x.com/a?b=1 <@123456> @someone mail me at a.b@c.com or +54 9 11 5555-1234 ```code here``` <:kekw:99999> ok"
	got := Clean(in)
	for _, bad := range []string{"http", "@", "123456", "c.com", "5555", "code here", "99999"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q survived in %q", bad, got)
		}
	}
	if !strings.Contains(got, ":kekw:") || !strings.Contains(got, "ok") {
		t.Fatalf("voice was removed with the noise: %q", got)
	}
}

// fake model: tells the three kinds of call apart by their system prompt.
type fake struct {
	mu      sync.Mutex
	calls   []provider.Request
	revised bool
	persona string
}

func (f *fake) Complete(_ context.Context, r provider.Request) (provider.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, r)
	switch {
	case r.System == draftSystem:
		return provider.Response{Text: "```\n---\nname: Snow\n---\n# Who you are\nterse, dry.\n```"}, nil
	case r.System == reviseSystem:
		f.revised = true
		return provider.Response{Text: "---\nname: Snow\n---\n# Who you are\nterse, dry, never more than a line."}, nil
	}
	if f.revised {
		w := strings.Fields(r.Messages[0].Content) // "Test: question number 17 about stuff"
		return provider.Response{Text: w[3] + " sure " + w[3] + " ok"}, nil
	}
	return provider.Response{Text: strings.Repeat("this is a very long and over explained answer ", 12)}, nil
}

func export() []Msg {
	var m []Msg
	for i := 0; i < 60; i++ {
		m = append(m, Msg{Author: "Ana", Text: fmt.Sprintf("question number %d about stuff, see https://spam.example/%d", i, i)})
		m = append(m, Msg{Author: "Snow", Text: fmt.Sprintf("nah thing %d <@42> lol mail s@x.io", i), Prev: fmt.Sprintf("question number %d about stuff, see https://spam.example/%d", i, i)})
	}
	return m
}

func TestDistillLearnsMeasuresRefinesAndWritesTheFiles(t *testing.T) {
	f := &fake{}
	dir := t.TempDir()
	var asked string
	res, err := Run(context.Background(), f, Options{Msgs: export(), As: "Snow", Dir: dir, Rounds: 3,
		Confirm: func(s string) bool { asked = s; return true }})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "snow" || res.Messages != 60 || len(res.Fails) != 0 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(asked, "will send") {
		t.Fatalf("the person was not told what leaves the machine: %q", asked)
	}
	if !f.revised {
		t.Fatal("a first draft outside the targets must be revised")
	}
	for _, name := range []string{"persona.md", "voice.json", "eval.txt"} {
		if _, err := os.Stat(filepath.Join(dir, "snow", name)); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "snow", "persona.md"))
	if !strings.Contains(string(b), "never more than a line") || strings.Contains(string(b), "```") {
		t.Fatalf("the best draft is not what was written:\n%s", b)
	}
	var tg voice.Targets
	tb, _ := os.ReadFile(filepath.Join(dir, "snow", "voice.json"))
	if json.Unmarshal(tb, &tg) != nil || tg.MedianWords[1] > 30 {
		t.Fatalf("targets were not derived from how Snow writes: %s", tb)
	}
	ev, _ := os.ReadFile(filepath.Join(dir, "snow", "eval.txt"))
	if !strings.Contains(string(ev), "question number") || strings.Contains(string(ev), "http") {
		t.Fatalf("eval inputs should be what people said to the real person, cleaned:\n%s", ev)
	}
	// nothing private reached the model
	for _, c := range f.calls {
		all := c.System
		for _, m := range c.Messages {
			all += m.Content
		}
		for _, bad := range []string{"https://", "<@42>", "s@x.io", "Ana"} {
			if strings.Contains(all, bad) && c.System == draftSystem {
				t.Fatalf("%q reached the model in the draft request", bad)
			}
		}
	}
}

func TestDecliningSendsNothingAndWritesNothing(t *testing.T) {
	f := &fake{}
	dir := t.TempDir()
	_, err := Run(context.Background(), f, Options{Msgs: export(), As: "Snow", Dir: dir, Confirm: func(string) bool { return false }})
	if err == nil || len(f.calls) != 0 {
		t.Fatalf("calls=%d err=%v", len(f.calls), err)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatal("files were written after declining")
	}
}

func TestTooLittleToLearnFromAndExistingPersonasAreRefused(t *testing.T) {
	f := &fake{}
	if _, err := Run(context.Background(), f, Options{Msgs: export()[:20], As: "Snow", Dir: t.TempDir()}); err == nil || len(f.calls) != 0 {
		t.Fatalf("a handful of messages was enough: %v", err)
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "snow"), 0o755)
	if _, err := Run(context.Background(), f, Options{Msgs: export(), As: "Snow", Dir: dir}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing persona overwritten: %v", err)
	}
	if _, err := Run(context.Background(), f, Options{Msgs: export(), As: "Snow", Dir: dir, ID: "../x"}); err == nil {
		t.Fatal("a path in the id was accepted")
	}
}
