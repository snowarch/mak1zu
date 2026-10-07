package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/snowarch/mak1zu/memory"
)

// The People tab: everyone she knows and everything she holds on each of them,
// readable and deletable by the person who runs her. Nothing here is hidden
// from the people it is about either: /memories and /diary show them the same.

func (s *Server) peopleList(w http.ResponseWriter, r *http.Request) {
	if s.Mem == nil {
		writeJSON(w, 200, []any{})
		return
	}
	sum, err := s.Mem.Summaries(r.Context())
	if err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, sum)
}

func (s *Server) personDetail(w http.ResponseWriter, r *http.Request) {
	if s.Mem == nil {
		fail(w, 404, errors.New("no memory"))
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	p, ok, err := s.Mem.Person(ctx, id)
	if err != nil || !ok {
		fail(w, 404, errors.New("no such person"))
		return
	}
	accs, _ := s.Mem.Accounts(ctx, id)
	mems, _ := s.Mem.AllMemories(ctx, id, 200)
	threads, _ := s.Mem.OpenThreads(ctx, id, 50)
	bits, _ := s.Mem.AllBits(ctx, id)
	diary, _ := s.Mem.AllDiary(ctx, id, 14)
	unsaid, _ := s.Mem.PendingUnsaidAny(ctx, id, time.Now())
	// nil slices would be JSON null; the page expects lists
	if accs == nil {
		accs = []memory.Account{}
	}
	if mems == nil {
		mems = []memory.Memory{}
	}
	if threads == nil {
		threads = []memory.Thread{}
	}
	if bits == nil {
		bits = []memory.Bit{}
	}
	if diary == nil {
		diary = []memory.DiaryEntry{}
	}
	if unsaid == nil {
		unsaid = []memory.Unsaid{}
	}
	writeJSON(w, 200, map[string]any{
		"person": p, "accounts": accs, "memories": mems, "threads": threads, "bits": bits, "diary": diary, "unsaid": unsaid,
	})
}

func (s *Server) personForget(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, _ := strconv.ParseInt(r.PathValue("n"), 10, 64)
	if s.Mem == nil {
		fail(w, 404, errors.New("no memory"))
		return
	}
	ctx := r.Context()
	var ok bool
	switch r.PathValue("what") {
	case "memory":
		ok, _ = s.Mem.Forget(ctx, id, n)
	case "thread":
		ok, _ = s.Mem.CloseThread(ctx, id, n)
	case "bit":
		ok, _ = s.Mem.DropBit(ctx, id, n)
	case "diary":
		ok = s.Mem.ClearDiary(ctx, id) == nil
	default:
		fail(w, 400, errors.New("memory, thread, bit or diary"))
		return
	}
	if !ok {
		fail(w, 404, errors.New("nothing to remove"))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) personErase(w http.ResponseWriter, r *http.Request) {
	if s.Mem == nil {
		fail(w, 404, errors.New("no memory"))
		return
	}
	if err := s.Mem.ForgetUser(r.Context(), r.PathValue("id")); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) personProfile(w http.ResponseWriter, r *http.Request) {
	if s.Mem == nil {
		fail(w, 404, errors.New("no memory"))
		return
	}
	var body struct{ Field, Value string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Mem.SetProfile(r.Context(), r.PathValue("id"), body.Field, body.Value); err != nil {
		fail(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
