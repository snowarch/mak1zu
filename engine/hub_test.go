package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// namedTransport is a fake transport with its own name.
type namedTransport struct {
	fakeTransport
	name  string
	local bool
}

func (n *namedTransport) Name() string { return n.name }
func (n *namedTransport) Local() bool  { return n.local }

func TestRepliesGoBackOnTheTransportTheyCameFrom(t *testing.T) {
	e, disc, _ := setup(t, say("from discord"), say("from the terminal"))
	loc := &namedTransport{name: "local", local: true}
	e.Add(loc)
	ctx := context.Background()
	e.Handle(ctx, dmFrom("1", "u1", "Alice", "hi")) // primary: discord
	e.Handle(ctx, sdk.Message{Transport: "local", ID: "2", ChannelID: "main", AuthorID: "me", AuthorName: "snow", Content: "hey", IsDM: true})
	if got := disc.texts(); len(got) != 1 || got[0] != "from discord" {
		t.Fatalf("discord got %v", got)
	}
	if got := loc.texts(); len(got) != 1 || got[0] != "from the terminal" {
		t.Fatalf("the local transport got %v", got)
	}
}

func TestOnePersonOnTwoTransportsIsOneMemoryWithProvenance(t *testing.T) {
	e, _, sc := setup(t, say("hi"), say("noted"), say("you told me on discord"))
	loc := &namedTransport{name: "local", local: true}
	e.Add(loc)
	ctx := context.Background()
	// she learns something on discord
	e.Handle(ctx, dmFrom("1", "u1", "Alice", "hello"))
	pid := personID(t, e, "u1")
	e.Mem.RememberFrom(ctx, "maki", "semantic", pid, "discord", "Alice keeps an axolotl named Pudding", 0.9, "")
	// the same human links the terminal and asks about it there
	code, _ := e.Mem.NewLinkCode(ctx, pid)
	if _, err := e.Mem.Link(ctx, "local", "me", "alice-terminal", code); err != nil {
		t.Fatal(err)
	}
	e.Handle(ctx, sdk.Message{Transport: "local", ID: "9", ChannelID: "main", AuthorID: "me", AuthorName: "alice-terminal", Content: "what is my axolotl called", IsDM: true})
	sys := sc.reqs[len(sc.reqs)-1].System
	if !strings.Contains(sys, "Pudding (told on discord") {
		t.Fatalf("the terminal did not get the discord memory with its source:\n%s", sys)
	}
}

func TestReminderFiresOnTheTransportItWasSetOn(t *testing.T) {
	e, disc, _ := setup(t, say("remember to stretch"))
	loc := &namedTransport{name: "local", local: true}
	e.Add(loc)
	ctx := context.Background()
	p, _ := e.Mem.Resolve(ctx, "local", "me", "snow")
	e.Mem.AddReminderOn(ctx, p.ID, "local", "main", "stretch", time.Now().Add(-time.Minute))
	e.FireReminders(ctx, time.Now())
	if len(loc.sent) != 1 || len(disc.sent) != 0 {
		t.Fatalf("reminder went to the wrong place: local=%d discord=%d", len(loc.sent), len(disc.sent))
	}
}

func TestSheWritesFirstOnTheTransportOfTheRoute(t *testing.T) {
	e, disc, _ := setup(t, say("hey"), say("so, the exam?"))
	loc := &namedTransport{name: "local", local: true}
	e.Add(loc)
	ctx := context.Background()
	e.Handle(ctx, sdk.Message{Transport: "local", ID: "1", ChannelID: "main", AuthorID: "me", AuthorName: "snow", Content: "hi", IsDM: true})
	pid := personID2(t, e, "local", "me")
	e.Mem.AddUnsaid(ctx, "maki", pid, "ask about the exam", 30*24*time.Hour)
	loc.sent = nil
	if n := e.ReachOut(ctx, atHour(2, 14)); n != 1 || len(loc.sent) != 1 || len(disc.sent) != 0 {
		t.Fatalf("n=%d local=%d discord=%d", n, len(loc.sent), len(disc.sent))
	}
}

func personID2(t *testing.T, e *Engine, transport, ext string) string {
	t.Helper()
	p, ok, _ := e.Mem.Lookup(context.Background(), transport, ext)
	if !ok {
		t.Fatal("no person")
	}
	return p.ID
}
