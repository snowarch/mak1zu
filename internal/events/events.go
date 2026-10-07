// Package events is the live feed behind the panel: what she heard, why she
// spoke or stayed quiet, what she said. It is an in-memory ring plus a
// fan-out. Nothing here is written to disk, so message previews never outlive
// the process.
package events

import (
	"sync"
	"time"
)

type Event struct {
	ID      int64     `json:"id"`
	TS      time.Time `json:"ts"`
	Type    string    `json:"type"` // heard | quiet | replied | incident | slip | system
	Place   string    `json:"place,omitempty"`
	Author  string    `json:"author,omitempty"`
	Text    string    `json:"text,omitempty"`   // what was said (preview)
	Reason  string    `json:"reason,omitempty"` // machine id, e.g. mention
	Why     string    `json:"why,omitempty"`    // the same, in words
	First   bool      `json:"first,omitempty"`  // first time she has heard from this person
	Words   int       `json:"words,omitempty"`
	Latency int64     `json:"latency_ms,omitempty"`
	Model   string    `json:"model,omitempty"`
	Tools   []string  `json:"tools,omitempty"`
	Saw     string    `json:"saw,omitempty"` // what went into the reply: memories, threads, prompt size
}

type Hub struct {
	mu   sync.Mutex
	ring []Event
	max  int
	next int64
	subs map[chan Event]struct{}
}

func NewHub(max int) *Hub {
	if max <= 0 {
		max = 300
	}
	return &Hub{max: max, subs: map[chan Event]struct{}{}}
}

// Emit stamps and publishes an event. It never blocks: a subscriber that
// cannot keep up simply misses events and can catch up with Since.
func (h *Hub) Emit(e Event) Event {
	if h == nil {
		return e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	e.ID = h.next
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	h.ring = append(h.ring, e)
	if len(h.ring) > h.max {
		h.ring = h.ring[len(h.ring)-h.max:]
	}
	for c := range h.subs {
		select {
		case c <- e:
		default:
		}
	}
	return e
}

// Since returns the buffered events newer than id, oldest first.
func (h *Hub) Since(id int64) []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Event{}
	for _, e := range h.ring {
		if e.ID > id {
			out = append(out, e)
		}
	}
	return out
}

// Subscribe returns a live channel and a cancel func.
func (h *Hub) Subscribe() (<-chan Event, func()) {
	c := make(chan Event, 64)
	h.mu.Lock()
	h.subs[c] = struct{}{}
	h.mu.Unlock()
	return c, func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}
}
