package panel

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/memory"
)

func TestPeopleTabShowsEverythingAndLetsTheOwnerRemoveIt(t *testing.T) {
	s, _ := newServer(t)
	mem, _ := memory.Open(":memory:")
	s.Mem = mem
	ctx := context.Background()
	p, _ := mem.Resolve(ctx, "discord", "1", "Alice")
	mem.RememberFrom(ctx, "maki", memory.Semantic, p.ID, "cli", "Alice likes Frieren", 0.8, "")
	tid, _ := mem.AddThread(ctx, p.ID, "discord", "exam thursday", time.Time{})
	bid, _ := mem.AddBit(ctx, "maki", p.ID, "discord", "the toaster incident", "toaster")
	mem.AddDiary(ctx, "maki", p.ID, "2026-10-06", "Alice was wired about the exam.")
	mem.AddUnsaid(ctx, "maki", p.ID, "ask how the exam went", time.Hour)
	mem.SetProfile(ctx, p.ID, "call_me", "Ren")

	var list []map[string]any
	w := do(s, "GET", "/api/people", "", nil)
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != 1 || list[0]["CallMe"] != "Ren" || list[0]["Memories"].(float64) != 1 || list[0]["Threads"].(float64) != 1 {
		t.Fatalf("%s", w.Body)
	}
	w = do(s, "GET", "/api/people/"+p.ID, "", nil)
	var d struct {
		Memories []memory.Memory
		Threads  []memory.Thread
		Bits     []memory.Bit
		Diary    []memory.DiaryEntry
		Unsaid   []memory.Unsaid
	}
	json.Unmarshal(w.Body.Bytes(), &d)
	if len(d.Memories) != 1 || d.Memories[0].Source != "cli" || len(d.Threads) != 1 || len(d.Bits) != 1 || len(d.Diary) != 1 || len(d.Unsaid) != 1 {
		t.Fatalf("detail incomplete: %s", w.Body)
	}
	if w := do(s, "GET", "/api/people/nobody", "", nil); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}

	// removing needs the CSRF header like every write
	if w := do(s, "DELETE", "/api/people/"+p.ID+"/thread/"+itoa(tid), "", nil); w.Code != 403 {
		t.Fatalf("delete without the header: %d", w.Code)
	}
	for _, path := range []string{"/thread/" + itoa(tid), "/bit/" + itoa(bid), "/memory/" + itoa(d.Memories[0].ID), "/diary/0"} {
		if w := do(s, "DELETE", "/api/people/"+p.ID+path, "", csrf); w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	if w := do(s, "DELETE", "/api/people/"+p.ID+"/thread/"+itoa(tid), "", csrf); w.Code != 404 {
		t.Fatal("removing twice should say there is nothing there")
	}
	if w := do(s, "PUT", "/api/people/"+p.ID+"/profile", `{"field":"role","value":"owner"}`, csrf); w.Code != 400 {
		t.Fatalf("the panel must not hand out the owner role through the profile: %d", w.Code)
	}
	if w := do(s, "PUT", "/api/people/"+p.ID+"/profile", `{"field":"quiet","value":"23:00-08:00"}`, csrf); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if w := do(s, "DELETE", "/api/people/"+p.ID, "", csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if got, _ := mem.Persons(ctx); len(got) != 0 {
		t.Fatalf("erase left %d people", len(got))
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
