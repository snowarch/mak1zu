// Package local is the transport for whoever runs the machine: the terminal
// UI and the web panel's chat both talk to her through it. It owns no network
// code. The panel serves it over HTTP for clients in other processes, and an
// embedded terminal UI uses it directly, so both paths go through one
// implementation.
package local

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// Channel is the one conversation the local person has with her.
const Channel = "main"

// Event is what a connected client sees.
type Event struct {
	Kind  string    `json:"kind"` // reply | typing | backlog
	Text  string    `json:"text,omitempty"`
	Files []string  `json:"files,omitempty"` // names only; the web chat fetches the content
	At    time.Time `json:"at"`
	ID    int64     `json:"id,omitempty"`
}

// HistoryFn returns the last n messages of the conversation, oldest first.
type HistoryFn func(ctx context.Context, n int) []sdk.Message

type Transport struct {
	name    string
	history HistoryFn
	// User is the name the person goes by on this machine (their chosen name
	// once she knows it, otherwise the account name).
	User func() string

	mu      sync.Mutex
	handler sdk.Handler
	subs    map[int]chan Event
	nextSub int
	backlog []Event // things she said while nobody was looking
	seq     int64
	files   map[string]sdk.File
}

// New makes the transport; botName is what she is called here. history may be
// nil (the conversation then starts empty each run).
func New(botName string, history HistoryFn) *Transport {
	return &Transport{name: botName, history: history, subs: map[int]chan Event{}, files: map[string]sdk.File{}}
}

func (t *Transport) Name() string       { return "local" }
func (t *Transport) Local() bool        { return true }
func (t *Transport) Self() sdk.Identity { return sdk.Identity{ID: "maki-local", Name: t.name} }
func (t *Transport) Typing(context.Context, string) error {
	t.publish(Event{Kind: "typing"})
	return nil
}

// SetHistory wires the conversation's persistent history after construction.
func (t *Transport) SetHistory(h HistoryFn) { t.history = h }

// Run waits for the engine's handler and then for the end of ctx.
func (t *Transport) Run(ctx context.Context, h sdk.Handler) error {
	t.mu.Lock()
	t.handler = h
	t.mu.Unlock()
	<-ctx.Done()
	return nil
}

// Say delivers a message from the person at the keyboard to the engine.
func (t *Transport) Say(ctx context.Context, text string) error {
	t.mu.Lock()
	h := t.handler
	t.seq++
	id := t.seq
	t.mu.Unlock()
	if h == nil {
		return fmt.Errorf("she is not listening yet")
	}
	authorName := "you"
	if t.User != nil {
		authorName = t.User()
	}
	// The turn outlives the call that started it: an HTTP request ends the moment
	// it is accepted, and a cancelled context would kill the model call mid-way.
	go h(context.WithoutCancel(ctx), sdk.Message{
		Transport: "local", ID: fmt.Sprintf("l%d", id), ChannelID: Channel, AuthorID: "local",
		AuthorName: authorName, Content: text, IsDM: true, Time: time.Now(),
	})
	return nil
}

// Send is her reply (or her writing first).
func (t *Transport) Send(_ context.Context, _ string, r sdk.Reply) error {
	ev := Event{Kind: "reply", Text: r.Text, At: time.Now()}
	t.mu.Lock()
	for _, f := range r.Files {
		t.files[f.Name] = f
		ev.Files = append(ev.Files, f.Name)
	}
	t.seq++
	ev.ID = t.seq
	quiet := len(t.subs) == 0
	if quiet {
		t.backlog = append(t.backlog, ev)
		if len(t.backlog) > 20 {
			t.backlog = t.backlog[len(t.backlog)-20:]
		}
	}
	t.mu.Unlock()
	if !quiet {
		t.publish(ev)
	}
	return nil
}

func (t *Transport) publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.subs {
		select {
		case c <- ev:
		default: // a slow client misses a typing blip, never blocks her
		}
	}
}

// File returns an attachment she sent, by name.
func (t *Transport) File(name string) (sdk.File, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f, ok := t.files[name]
	return f, ok
}

// Subscribe starts a client's stream. Whatever she said while nobody was
// connected arrives first, marked as backlog: she was here while you were gone.
func (t *Transport) Subscribe() (<-chan Event, func()) {
	c := make(chan Event, 64)
	t.mu.Lock()
	id := t.nextSub
	t.nextSub++
	t.subs[id] = c
	pending := t.backlog
	t.backlog = nil
	t.mu.Unlock()
	for _, ev := range pending {
		ev.Kind = "backlog"
		c <- ev
	}
	return c, func() {
		t.mu.Lock()
		delete(t.subs, id)
		t.mu.Unlock()
	}
}

// History satisfies sdk.Transport for the engine's context building.
func (t *Transport) History(ctx context.Context, _ string, n int) ([]sdk.Message, error) {
	if t.history == nil {
		return nil, nil
	}
	return t.history(ctx, n), nil
}
