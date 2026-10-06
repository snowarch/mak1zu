package panel

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/persona"
)

// FaceNames is the contract with the avatar artwork: the only names Face ever
// returns. Art may lag behind; the panel falls back to "neutral", then to a lamp.
var FaceNames = []string{"neutral", "shy", "deadpan", "smug", "amused", "irritated", "sleepy", "wired", "alarm", "embarrassed", "waitwait", "sad"}

// Face picks her expression from what she feels and what just happened.
// Priority: a recent problem, the pause switch, a recent moment, then mood.
func Face(m persona.MoodState, paused bool, last *events.Event, now time.Time) string {
	if last != nil {
		age := now.Sub(last.TS)
		switch {
		case last.Type == "incident" && age < 10*time.Second:
			return "alarm"
		case paused:
			return "sleepy"
		case last.Type == "heard" && age < 5*time.Second && (last.Reason == "mention" || last.Reason == "dm") && now.Hour() < 5:
			return "embarrassed" // woken at night
		case last.Type == "replied" && age < 5*time.Second:
			return "amused"
		case last.Type == "quiet" && last.Reason == "dice" && age < 4*time.Second:
			return "deadpan"
		}
	}
	if paused {
		return "sleepy"
	}
	switch m.Name {
	case "amused":
		if m.Intensity > 0.6 {
			return "smug"
		}
		return "amused"
	case "irritated":
		if m.Intensity > 0.6 {
			return "irritated"
		}
		return "deadpan"
	case "sleepy", "wired":
		return m.Name
	}
	return "neutral"
}

// lastMoment is the newest event that can change her face.
func (s *Server) lastMoment() *events.Event {
	if s.Ev == nil {
		return nil
	}
	evs := s.Ev.Since(0)
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != "system" {
			return &evs[i]
		}
	}
	return nil
}

// avatar serves a pre-rendered expression PNG when the artwork package is
// wired in. Until then it is a clean 404 and the panel shows its lamp.
func (s *Server) avatar(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".png")
	known := false
	for _, n := range FaceNames {
		known = known || n == name
	}
	if !known || s.Avatar == nil {
		http.NotFound(w, r)
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 || size > 1024 {
		size = 128
	}
	b, ok := s.Avatar(name, size)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(b)
}
