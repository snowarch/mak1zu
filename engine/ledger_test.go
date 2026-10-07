package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

func TestAgoSpeaksLikeAPerson(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		d    time.Duration
		want string
	}{{2 * time.Hour, "today"}, {30 * time.Hour, "yesterday"}, {3 * 24 * time.Hour, "3 days ago"}, {21 * 24 * time.Hour, "3 weeks ago"}, {200 * 24 * time.Hour, "7 months ago"}} {
		if got := ago(now.Add(-c.d), now); got != c.want {
			t.Errorf("%v: %q want %q", c.d, got, c.want)
		}
	}
}

func TestMemoryProvenanceShowsOnlyWhenItIsElsewhere(t *testing.T) {
	now := time.Now()
	m := memory.Memory{Content: "Ren is learning Go", Source: "cli", Created: now.Add(-72 * time.Hour).UTC().Format(time.RFC3339)}
	if got := describeMemory(m, "discord", now); got != "Ren is learning Go (told on cli, 3 days ago)" {
		t.Fatal(got)
	}
	if got := describeMemory(m, "cli", now); got != "Ren is learning Go (3 days ago)" {
		t.Fatal(got)
	}
	m.Source = ""
	if got := describeMemory(m, "discord", now); strings.Contains(got, "told on") {
		t.Fatalf("a legacy memory has no known source: %q", got)
	}
}

func TestLedgerReachesPromptAndBitRestsAfterCallback(t *testing.T) {
	e, _, sc := setup(t, say("hey"), say("lol the toaster again"), say("fine"))
	ctx := context.Background()
	e.Handle(ctx, msg("1", "hi"))
	pid := personID(t, e, "u1")
	e.Mem.AddThread(ctx, pid, "discord", "Alice's exam is thursday", time.Time{})
	e.Mem.AddBit(ctx, "maki", pid, "discord", "the toaster incident", "toaster")
	e.Mem.RememberFrom(ctx, "maki", memory.Semantic, pid, "cli", "Alice learn Go at night", 0.9, "")

	e.Handle(ctx, msg("2", "so, what do I learn at night, and the exam"))
	sys := sc.reqs[len(sc.reqs)-1].System
	for _, want := range []string{"<open_threads>", "exam is thursday", "<running_bits>", "the toaster incident", "told on cli"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("prompt missing %q:\n%s", want, sys)
		}
	}
	// her reply used the bit: it must rest on the next turn
	e.Handle(ctx, msg("3", "anything else?"))
	if strings.Contains(sc.reqs[len(sc.reqs)-1].System, "the toaster incident") {
		t.Fatal("a bit she just used came straight back")
	}
}

func TestAnotherPersonNeverSeesMyThreads(t *testing.T) {
	e, _, sc := setup(t, say("a"), say("b"))
	ctx := context.Background()
	e.Handle(ctx, msg("1", "hi"))
	e.Mem.AddThread(ctx, personID(t, e, "u1"), "discord", "Alice's secret surgery", time.Time{})
	b := msg("2", "hello")
	b.AuthorID, b.AuthorName = "u2", "Bob"
	e.Handle(ctx, b)
	if strings.Contains(sc.reqs[1].System, "surgery") {
		t.Fatal("alice's thread reached bob's prompt")
	}
}

func TestThreadAndBitToolsAndCommands(t *testing.T) {
	call := func(provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{
			{ID: "1", Name: "open_thread", Args: `{"text":"Alice has a driving test","due":"2031-05-02"}`},
			{ID: "2", Name: "note_bit", Args: `{"text":"calls the cat Sir Bonk","trigger":"bonk"}`},
			{ID: "3", Name: "set_profile", Args: `{"field":"checkins","value":"off"}`},
		}}, nil
	}
	e, _, _ := setup(t, call, say("on it"))
	ctx := context.Background()
	e.Handle(ctx, msg("1", "driving test next month, the cat is Sir Bonk, and stop checking in on me"))
	pid := personID(t, e, "u1")
	if th, _ := e.Mem.OpenThreads(ctx, pid, 5); len(th) != 1 || !strings.HasPrefix(th[0].Due, "2031-05-02") {
		t.Fatalf("thread: %+v", th)
	}
	if p, _, _ := e.Mem.Person(ctx, pid); p.WantsCheckins() {
		t.Fatal("'stop checking in' was not stored")
	}
	call2 := sdk.CommandCall{UserID: "u1", UserName: "Alice"}
	view := findCmd(e, "memories").Run(ctx, call2)
	for _, want := range []string{"driving test", "Sir Bonk", "will not start conversations"} {
		if !strings.Contains(view, want) {
			t.Fatalf("/memories hides %q:\n%s", want, view)
		}
	}
	call2.Args = map[string]string{"what": "bit 1"}
	if out := findCmd(e, "forget").Run(ctx, call2); out != "gone" {
		t.Fatal(out)
	}
	call2.Args = map[string]string{"what": "thread 1"}
	if out := findCmd(e, "forget").Run(ctx, call2); out != "gone" {
		t.Fatal(out)
	}
	// bob cannot drop alice's
	bob := sdk.CommandCall{UserID: "u2", UserName: "Bob", Args: map[string]string{"what": "thread 1"}}
	if out := findCmd(e, "forget").Run(ctx, bob); !strings.Contains(out, "no such") {
		t.Fatalf("bob touched alice's thread: %s", out)
	}
}

func TestParseExtractionAcceptsObjectArrayAndProse(t *testing.T) {
	f, th := parseExtraction("sure!\n{\"facts\":[\"Ren likes Frieren\"],\"threads\":[{\"text\":\"Ren has an exam\",\"due\":\"2026-10-09\"}]}")
	if len(f) != 1 || len(th) != 1 || th[0].Due != "2026-10-09" {
		t.Fatalf("%v %v", f, th)
	}
	f, th = parseExtraction(`["Ren plays chess"]`)
	if len(f) != 1 || len(th) != 0 {
		t.Fatalf("legacy array: %v %v", f, th)
	}
	if f, th = parseExtraction("nothing durable here"); len(f)+len(th) != 0 {
		t.Fatalf("junk parsed: %v %v", f, th)
	}
}
