// Package telemetry keeps a bounded in-memory ring of turn records for the
// panel and, optionally, appends them to a JSONL file. It never stores
// prompts, credentials or user message text, only shape and timing.
package telemetry

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Record struct {
	TS       time.Time `json:"ts"`
	Kind     string    `json:"kind"` // turn | incident | skipped
	Persona  string    `json:"persona,omitempty"`
	Channel  string    `json:"channel,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Model    string    `json:"model,omitempty"`
	Latency  float64   `json:"latency_s,omitempty"`
	Words    int       `json:"words,omitempty"`
	Robotic  []string  `json:"robotic_hits,omitempty"`
	Tools    []string  `json:"tools,omitempty"`
	Rounds   int       `json:"rounds,omitempty"`
	Cause    string    `json:"cause,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

type Log struct {
	mu   sync.Mutex
	ring []Record
	max  int
	f    *os.File
}

func New(max int, path string) *Log {
	l := &Log{max: max}
	if path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			l.f = f
		}
	}
	return l
}

func (l *Log) Add(r Record) {
	if r.TS.IsZero() {
		r.TS = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ring = append(l.ring, r)
	if len(l.ring) > l.max {
		l.ring = l.ring[len(l.ring)-l.max:]
	}
	if l.f != nil {
		b, _ := json.Marshal(r)
		l.f.Write(append(b, '\n'))
	}
}

// Recent returns up to n records, newest last, optionally filtered by kind.
func (l *Log) Recent(n int, kind string) []Record {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Record{} // never nil: the panel iterates the JSON
	for _, r := range l.ring {
		if kind == "" || r.Kind == kind {
			out = append(out, r)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func (l *Log) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		l.f.Close()
		l.f = nil
	}
}
