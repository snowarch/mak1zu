// Package tui is the terminal chat. It is a client of the local conversation:
// attached to a running daemon over its HTTP API, or driving an engine it runs
// itself, so a first run needs no daemon at all. Both go through the Conn
// interface, so the screen code does not know which one it has.
package tui

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/transport/local"
)

// Line is one message in the conversation.
type Line struct {
	Who  string // you | her
	Text string
}

// Conn is the conversation as the screen sees it.
type Conn interface {
	Say(ctx context.Context, text string) error
	Stream(ctx context.Context) (<-chan local.Event, error)
	History(ctx context.Context, n int) ([]Line, error)
	// Command runs a slash command as this person. ok is false when no such command exists.
	Command(ctx context.Context, name, arg string) (out string, ok bool, err error)
	// Where says what the screen is attached to, in a few words.
	Where() string
}

// ---- attached to a daemon ----

// Remote talks to a running daemon's panel API.
type Remote struct {
	Base  string // http://127.0.0.1:8787
	Token string
	HTTP  *http.Client
}

func (r *Remote) client() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return &http.Client{}
}

func (r *Remote) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Mak1zu", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.Token != "" {
		req.Header.Set("Authorization", "Bearer "+r.Token)
	}
	return r.client().Do(req)
}

func (r *Remote) Where() string { return "attached to " + strings.TrimPrefix(r.Base, "http://") }

// Probe reports whether a mak1zu daemon with the local chat answers at Base.
func (r *Remote) Probe(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	resp, err := r.do(ctx, http.MethodGet, "/api/chat/history?n=1", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		return false
	}
	var v []json.RawMessage
	return json.NewDecoder(resp.Body).Decode(&v) == nil
}

func (r *Remote) Say(ctx context.Context, text string) error {
	resp, err := r.do(ctx, http.MethodPost, "/api/chat/say", map[string]string{"text": text})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 202 {
		return httpError(resp)
	}
	return nil
}

func httpError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
	if e.Error != "" {
		return errors.New(e.Error)
	}
	return fmt.Errorf("the daemon answered %s", resp.Status)
}

func (r *Remote) Stream(ctx context.Context) (<-chan local.Event, error) {
	resp, err := r.do(ctx, http.MethodGet, "/api/chat/stream", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, httpError(resp)
	}
	out := make(chan local.Event, 32)
	go func() {
		defer resp.Body.Close()
		defer close(out)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev local.Event
			if json.Unmarshal([]byte(line[6:]), &ev) == nil {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (r *Remote) History(ctx context.Context, n int) ([]Line, error) {
	resp, err := r.do(ctx, http.MethodGet, fmt.Sprintf("/api/chat/history?n=%d", n), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, httpError(resp)
	}
	var raw []struct{ Who, Text string }
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Line, len(raw))
	for i, x := range raw {
		out[i] = Line{Who: x.Who, Text: x.Text}
	}
	return out, nil
}

func (r *Remote) Command(ctx context.Context, name, arg string) (string, bool, error) {
	resp, err := r.do(ctx, http.MethodPost, "/api/chat/command", map[string]string{"name": name, "arg": arg})
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return "", false, nil
	}
	if resp.StatusCode != 200 {
		return "", false, httpError(resp)
	}
	var v struct{ Out string }
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", false, err
	}
	return v.Out, true, nil
}

// ---- driving an engine in this process ----

// Embedded drives the local transport of an engine this process runs.
type Embedded struct {
	T      *local.Transport
	Run    func(ctx context.Context, name, arg string) (string, bool)
	Detail string
}

func (e *Embedded) Where() string                           { return e.Detail }
func (e *Embedded) Say(ctx context.Context, t string) error { return e.T.Say(ctx, t) }

func (e *Embedded) Stream(ctx context.Context) (<-chan local.Event, error) {
	ch, cancel := e.T.Subscribe()
	out := make(chan local.Event, 32)
	go func() {
		defer cancel()
		defer close(out)
		for {
			select {
			case ev := <-ch:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (e *Embedded) History(ctx context.Context, n int) ([]Line, error) {
	msgs, _ := e.T.History(ctx, "", n)
	out := make([]Line, 0, len(msgs))
	for _, m := range msgs {
		who := "you"
		if m.AuthorID == e.T.Self().ID {
			who = "her"
		}
		out = append(out, Line{Who: who, Text: m.Content})
	}
	return out, nil
}

func (e *Embedded) Command(ctx context.Context, name, arg string) (string, bool, error) {
	out, ok := e.Run(ctx, name, arg)
	return out, ok, nil
}
