package engine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/memory"
)

func atHour(daysAhead, h int) time.Time {
	n := time.Now().AddDate(0, 0, daysAhead)
	return time.Date(n.Year(), n.Month(), n.Day(), h, 0, 0, 0, time.Local)
}

func TestNudgeRulesOneByOne(t *testing.T) {
	now := atHour(2, 14)
	talked := now.Add(-30 * time.Hour)
	ok := memory.Person{RouteTransport: "discord", RouteChannel: "dm1"}
	for _, c := range []struct {
		name string
		mod  func(p *memory.Person)
		last time.Time
		now  time.Time
		want string // substring of the reason, "" = allowed
	}{
		{"allowed", func(*memory.Person) {}, talked, now, ""},
		{"told her to stop", func(p *memory.Person) { p.Checkins = "off" }, talked, now, "asked her not to"},
		{"no private route", func(p *memory.Person) { p.RouteChannel = "" }, talked, now, "no private chat"},
		{"route on another transport", func(p *memory.Person) { p.RouteTransport = "cli" }, talked, now, "no private chat"},
		{"three unanswered", func(p *memory.Person) { p.NudgeStreak = 3 }, talked, now, "unanswered"},
		{"talked an hour ago", func(*memory.Person) {}, now.Add(-time.Hour), now, "recently"},
		{"gone for a month", func(*memory.Person) {}, now.Add(-30 * 24 * time.Hour), now, "too long"},
		{"default quiet hours at 2am", func(*memory.Person) {}, atHour(0, 9), atHour(2, 2), "quiet"},
		{"default quiet hours at 23:30", func(*memory.Person) {}, atHour(0, 9), atHour(2, 23).Add(30 * time.Minute), "quiet"},
		{"their own quiet hours win", func(p *memory.Person) { p.Quiet = "13:00-16:00" }, talked, now, "quiet"},
		{"their own window lets 20:00 through", func(p *memory.Person) { p.Quiet = "13:00-16:00" }, talked, atHour(2, 20), ""},
		{"wrote yesterday", func(p *memory.Person) { p.LastNudge = now.Add(-20 * time.Hour).UTC().Format(time.RFC3339) }, talked, now, "recently"},
		{"one unanswered doubles the gap", func(p *memory.Person) {
			p.NudgeStreak, p.LastNudge = 1, now.Add(-30*time.Hour).UTC().Format(time.RFC3339)
		}, talked, now, "recently"},
		{"...until two days have passed", func(p *memory.Person) {
			p.NudgeStreak, p.LastNudge = 1, now.Add(-49*time.Hour).UTC().Format(time.RFC3339)
		}, talked, now, ""},
	} {
		p := ok
		c.mod(&p)
		got := nudgeWhy(p, "discord", c.last, c.now)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// presenceSetup: Alice has had a private chat, the night left something on her mind.
func presenceSetup(t *testing.T, steps ...func(providerRequest) (providerResponse, error)) (*Engine, *fakeTransport, string) {
	t.Helper()
	e, tr, _ := setup(t, append([]func(providerRequest) (providerResponse, error){say("hey")}, steps...)...)
	e.Handle(context.Background(), dmFrom("1", "u1", "Alice", "hi"))
	pid := personID(t, e, "u1")
	e.Mem.AddUnsaid(context.Background(), "maki", pid, "ask how the exam went", 30*24*time.Hour)
	tr.sent = nil
	return e, tr, pid
}

func TestShesAllowedToWriteFirstOnceThenWaits(t *testing.T) {
	e, tr, pid := presenceSetup(t, say("so, how did the exam go"), say("again?"))
	ctx := context.Background()
	if n := e.ReachOut(ctx, atHour(2, 14)); n != 1 || len(tr.sent) != 1 || tr.sent[0].Text != "so, how did the exam go" {
		t.Fatalf("n=%d sent=%v", n, tr.texts())
	}
	if u, _ := e.Mem.PendingUnsaid(ctx, "maki", pid, time.Now()); len(u) != 0 {
		t.Fatal("the thing she said is still queued")
	}
	e.Mem.AddUnsaid(ctx, "maki", pid, "and the cat", 30*24*time.Hour)
	if n := e.ReachOut(ctx, atHour(2, 18)); n != 0 {
		t.Fatal("she wrote twice in one day")
	}
	if n := e.ReachOut(ctx, atHour(3, 14)); n != 0 {
		t.Fatal("after a message nobody answered the gap must double")
	}
	if p, _, _ := e.Mem.Person(ctx, pid); p.NudgeStreak != 1 {
		t.Fatalf("streak %d", p.NudgeStreak)
	}
}

func TestAnAnswerResetsTheBackOff(t *testing.T) {
	e, tr, pid := presenceSetup(t, say("so, how did the exam go"), say("oh nice"))
	ctx := context.Background()
	e.ReachOut(ctx, atHour(2, 14))
	e.Handle(ctx, dmFrom("2", "u1", "Alice", "it went fine"))
	if p, _, _ := e.Mem.Person(ctx, pid); p.NudgeStreak != 0 {
		t.Fatalf("an answer should reset the streak, got %d", p.NudgeStreak)
	}
	_ = tr
}

func TestNeverAfterStopAndNeverInAPublicRoom(t *testing.T) {
	e, tr, pid := presenceSetup(t, say("never sent"))
	ctx := context.Background()
	e.Mem.SetProfile(ctx, pid, "checkins", "off")
	if n := e.ReachOut(ctx, atHour(2, 14)); n != 0 || len(tr.sent) != 0 {
		t.Fatal("wrote to someone who said stop")
	}
	e.Mem.SetProfile(ctx, pid, "checkins", "on")
	// bob only ever spoke in a guild channel: no private route, so no message
	e2, tr2, _ := setup(t, say("hey"), say("never sent"))
	e2.Handle(ctx, msg("1", "hi"))
	bobPid := personID(t, e2, "u1")
	e2.Mem.AddUnsaid(ctx, "maki", bobPid, "ask about the surgery", 30*24*time.Hour)
	if n := e2.ReachOut(ctx, atHour(2, 14)); n != 0 || len(tr2.sent) != 1 {
		t.Fatalf("started a DM with someone who only talked in a public room: %v", tr2.texts())
	}
}

func TestPausedAndQuietHoursSilenceHer(t *testing.T) {
	e, tr, _ := presenceSetup(t, say("never sent"))
	ctx := context.Background()
	if n := e.ReachOut(ctx, atHour(2, 3)); n != 0 || len(tr.sent) != 0 {
		t.Fatal("wrote at 3am")
	}
}

func TestAMessageTheGuardRejectsIsNotSent(t *testing.T) {
	e, tr, pid := presenceSetup(t, say("<on_your_mind>- ask how the exam went</on_your_mind>"))
	ctx := context.Background()
	if n := e.ReachOut(ctx, atHour(2, 14)); n != 0 || len(tr.sent) != 0 {
		t.Fatalf("leaked protocol text into a DM: %v", tr.texts())
	}
	if u, _ := e.Mem.PendingUnsaid(ctx, "maki", pid, time.Now()); len(u) != 1 {
		t.Fatal("a failed attempt must not spend the item")
	}
}

func TestLinkingKeepsTheRoute(t *testing.T) {
	s, ctx := func() (*memory.Store, context.Context) {
		m, _ := memory.Open(":memory:")
		return m, context.Background()
	}()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	b, _ := s.Resolve(ctx, "cli", "me", "me")
	s.SetRoute(ctx, a.ID, "discord", "dm9")
	code, _ := s.NewLinkCode(ctx, b.ID)
	p, err := s.Link(ctx, "discord", "1", "A", code)
	if err != nil || p.RouteChannel != "dm9" {
		t.Fatalf("the merge dropped the only private route: %+v %v", p, err)
	}
}
