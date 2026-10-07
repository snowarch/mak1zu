package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The local chat: the person at this machine talking to her. The web panel
// and the terminal UI use the same four endpoints, which is why the terminal
// can attach to a running daemon instead of starting a second engine.

func (s *Server) chatSay(w http.ResponseWriter, r *http.Request) {
	if s.Chat == nil {
		fail(w, 404, errors.New("the local chat is not available in this build"))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		fail(w, 400, errors.New("say something"))
		return
	}
	if err := s.Chat.Say(r.Context(), body.Text); err != nil {
		fail(w, 503, err)
		return
	}
	writeJSON(w, 202, map[string]bool{"ok": true})
}

// chatStream is server-sent events: replies, typing blips, and first of all
// whatever she said while nobody was connected.
func (s *Server) chatStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok || s.Chat == nil {
		fail(w, 500, errors.New("streaming unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Chat.Subscribe()
	defer cancel()
	fmt.Fprintf(w, "retry: 3000\n: %s\n\n", strings.Repeat(" ", 2048)) // see eventsStream
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}

func (s *Server) chatHistory(w http.ResponseWriter, r *http.Request) {
	if s.Chat == nil {
		writeJSON(w, 200, []any{})
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 200 {
		n = 40
	}
	msgs, _ := s.Chat.History(r.Context(), "", n)
	type line struct {
		Who  string `json:"who"` // you | her
		Text string `json:"text"`
	}
	out := make([]line, 0, len(msgs))
	for _, m := range msgs {
		who := "you"
		if m.AuthorID == s.Chat.Self().ID {
			who = "her"
		}
		out = append(out, line{who, m.Content})
	}
	writeJSON(w, 200, out)
}

func (s *Server) chatFile(w http.ResponseWriter, r *http.Request) {
	if s.Chat == nil {
		http.NotFound(w, r)
		return
	}
	f, ok := s.Chat.File(r.PathValue("name"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	// She wrote this file: never let the browser render it as a page of ours.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	w.Write(f.Data)
}
