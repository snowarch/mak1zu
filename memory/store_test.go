package memory

import (
	"context"
	"testing"
	"time"
)

func TestRecallIsScopedToSpeaker(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s.Remember(ctx, "maki", Semantic, "alice", "alice keeps a pet axolotl named Pudding", 0.8, "pet")
	s.Remember(ctx, "maki", Semantic, "bob", "bob is afraid of axolotls", 0.8, "pet")
	s.Remember(ctx, "maki", Semantic, Global, "the server mascot is an axolotl", 0.5, "")
	got, err := s.Recall(ctx, "maki", "alice", "tell me about the axolotl", 6)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range got {
		if m.UserID == "bob" {
			t.Fatalf("leaked bob's private memory to alice: %q", m.Content)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want alice's + global memory, got %d: %+v", len(got), got)
	}
}

func TestRememberDedupes(t *testing.T) {
	s, _ := Open(":memory:")
	ctx := context.Background()
	a, _ := s.Remember(ctx, "maki", Semantic, "u", "Likes Frieren", 0.5, "")
	b, _ := s.Remember(ctx, "maki", Semantic, "u", "likes frieren", 0.5, "")
	if a != b {
		t.Fatalf("duplicate stored: %d vs %d", a, b)
	}
}

func TestFTSQueryCannotInjectOperators(t *testing.T) {
	if q := ftsQuery(`foo" OR NEAR(bar) * baz`); q != `"foo" OR "near" OR "bar" OR "baz"` {
		t.Fatalf("unexpected query %s", q)
	}
	s, _ := Open(":memory:")
	if _, err := s.Recall(context.Background(), "p", "u", `") DROP TABLE memories; --`, 3); err != nil {
		t.Fatal(err)
	}
}

func TestForgetUserErasesEverything(t *testing.T) {
	s, _ := Open(":memory:")
	ctx := context.Background()
	s.Remember(ctx, "p", Semantic, "u", "secret thing", 0.9, "")
	s.Touch(ctx, "p", "u", "U")
	s.LogTurn(ctx, "p", "u", "c", "hi", "yo")
	if err := s.ForgetUser(ctx, "u"); err != nil {
		t.Fatal(err)
	}
	st := s.Stats(ctx)
	if st["memories"]+st["people"]+st["relationships"]+st["turns"] != 0 {
		t.Fatalf("leftovers: %v", st)
	}
}

func TestRelationshipGrows(t *testing.T) {
	s, _ := Open(":memory:")
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		s.Touch(ctx, "p", "u", "U")
	}
	r, _ := s.Relationship(ctx, "p", "u")
	if r.Interactions != 30 || r.Familiarity <= 0.2 {
		t.Fatalf("%+v", r)
	}
}

func TestRemindersFireOnce(t *testing.T) {
	s, _ := Open(":memory:")
	ctx := context.Background()
	now := time.Now()
	s.AddReminder(ctx, "u", "c", "water the plant", now.Add(-time.Minute))
	s.AddReminder(ctx, "u", "c", "later", now.Add(time.Hour))
	got, _ := s.DueReminders(ctx, now)
	if len(got) != 1 || got[0].Content != "water the plant" {
		t.Fatalf("%+v", got)
	}
	if again, _ := s.DueReminders(ctx, now); len(again) != 0 {
		t.Fatal("fired twice")
	}
}
