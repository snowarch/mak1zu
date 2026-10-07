package memory

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestThreadsAreDedupedCappedAndPrivate(t *testing.T) {
	s, ctx := open(t), context.Background()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	b, _ := s.Resolve(ctx, "discord", "2", "B")
	id1, _ := s.AddThread(ctx, a.ID, "discord", "exam on thursday", time.Time{})
	id2, _ := s.AddThread(ctx, a.ID, "cli", "Exam on Thursday", time.Time{})
	if id1 != id2 {
		t.Fatal("same open thread opened twice")
	}
	if ok, _ := s.CloseThread(ctx, b.ID, id1); ok {
		t.Fatal("B closed A's thread")
	}
	for i := 0; i < 20; i++ {
		s.AddThread(ctx, a.ID, "discord", fmt.Sprintf("thing %d", i), time.Time{})
	}
	th, _ := s.OpenThreads(ctx, a.ID, 50)
	if len(th) != maxOpenThreads {
		t.Fatalf("cap not enforced: %d open", len(th))
	}
	if ok, _ := s.CloseThread(ctx, a.ID, th[0].ID); !ok {
		t.Fatal("owner could not close their thread")
	}
	if th2, _ := s.OpenThreads(ctx, a.ID, 50); len(th2) != maxOpenThreads-1 {
		t.Fatal("closed thread still listed")
	}
	if got, _ := s.OpenThreads(ctx, b.ID, 50); len(got) != 0 {
		t.Fatal("B sees A's threads")
	}
}

func TestDatedThreadsComeFirst(t *testing.T) {
	s, ctx := open(t), context.Background()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	s.AddThread(ctx, a.ID, "", "undated worry", time.Time{})
	s.AddThread(ctx, a.ID, "", "later", time.Now().Add(72*time.Hour))
	s.AddThread(ctx, a.ID, "", "sooner", time.Now().Add(24*time.Hour))
	th, _ := s.OpenThreads(ctx, a.ID, 10)
	if th[0].Text != "sooner" || th[1].Text != "later" || th[2].Text != "undated worry" {
		t.Fatalf("order: %+v", th)
	}
}

func TestBitRestsAfterUseAndComesBack(t *testing.T) {
	s, ctx := open(t), context.Background()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	s.AddBit(ctx, "maki", a.ID, "discord", "the toaster incident", "toaster")
	s.AddBit(ctx, "maki", a.ID, "discord", "no trigger bit", "")
	t0 := time.Now()
	ready, _ := s.BitsReady(ctx, "maki", a.ID, t0, 5)
	if len(ready) != 2 {
		t.Fatalf("fresh bits should be ready: %+v", ready)
	}
	if n, _ := s.NoteBitUse(ctx, "maki", a.ID, "ha, the Toaster again", t0); n != 1 {
		t.Fatalf("trigger not detected case-insensitively: %d", n)
	}
	if ready, _ = s.BitsReady(ctx, "maki", a.ID, t0.Add(time.Hour), 5); len(ready) != 1 || ready[0].Text != "no trigger bit" {
		t.Fatalf("a used bit must rest: %+v", ready)
	}
	if ready, _ = s.BitsReady(ctx, "maki", a.ID, t0.Add(BitCooldown+time.Hour), 5); len(ready) != 2 {
		t.Fatalf("the bit should come back after the cooldown: %+v", ready)
	}
	// another persona and another person have their own
	if o, _ := s.BitsReady(ctx, "rem", a.ID, t0, 5); len(o) != 0 {
		t.Fatal("bits leaked across personas")
	}
}

func TestBitsAreCappedKeepingTheMostUsed(t *testing.T) {
	s, ctx := open(t), context.Background()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	s.AddBit(ctx, "maki", a.ID, "", "the old favourite", "favourite")
	s.NoteBitUse(ctx, "maki", a.ID, "favourite!", time.Now())
	for i := 0; i < 20; i++ {
		s.AddBit(ctx, "maki", a.ID, "", fmt.Sprintf("bit %d", i), "")
	}
	all, _ := s.Bits(ctx, "maki", a.ID)
	if len(all) != maxBits {
		t.Fatalf("cap: %d", len(all))
	}
	kept := false
	for _, b := range all {
		kept = kept || b.Text == "the old favourite"
	}
	if !kept {
		t.Fatal("the most used bit was evicted by newer ones")
	}
}

func TestBoundariesValidateAndQuietHoursCrossMidnight(t *testing.T) {
	s, ctx := open(t), context.Background()
	a, _ := s.Resolve(ctx, "discord", "1", "A")
	if err := s.SetProfile(ctx, a.ID, "checkins", "maybe"); err == nil {
		t.Fatal("bad checkins value accepted")
	}
	if err := s.SetProfile(ctx, a.ID, "quiet", "late"); err == nil {
		t.Fatal("bad quiet value accepted")
	}
	s.SetProfile(ctx, a.ID, "checkins", "off")
	s.SetProfile(ctx, a.ID, "quiet", "23:00-08:00")
	s.SetProfile(ctx, a.ID, "tz", "UTC")
	p, _, _ := s.Person(ctx, a.ID)
	if p.WantsCheckins() {
		t.Fatal("checkins off ignored")
	}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 7, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		h, m int
		want bool
	}{{23, 0, true}, {2, 30, true}, {7, 59, true}, {8, 0, false}, {12, 0, false}, {22, 59, false}} {
		if got := p.InQuietHours(at(c.h, c.m)); got != c.want {
			t.Errorf("%02d:%02d quiet=%v want %v", c.h, c.m, got, c.want)
		}
	}
	// stricter boundary survives a merge
	b, _ := s.Resolve(ctx, "cli", "me", "me")
	code, _ := s.NewLinkCode(ctx, b.ID)
	if _, err := s.Link(ctx, "discord", "1", "A", code); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := s.Person(ctx, b.ID); m.WantsCheckins() || m.Quiet != "23:00-08:00" {
		t.Fatalf("a merge must not lose 'no check-ins': %+v", m)
	}
}

func TestMemoryKeepsWhereItWasLearned(t *testing.T) {
	s, ctx := open(t), context.Background()
	s.RememberFrom(ctx, "maki", Semantic, "u", "cli", "u is learning Go on the terminal", 0.8, "")
	got, _ := s.Recall(ctx, "maki", "u", "", 5)
	if len(got) != 1 || got[0].Source != "cli" {
		t.Fatalf("source lost: %+v", got)
	}
}
