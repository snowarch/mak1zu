// Package cli is a terminal transport: talk to the companion without Discord.
// It is also the fastest way to develop a persona or a plugin.
package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

type Transport struct {
	In      io.Reader
	Out     io.Writer
	User    string
	BotName string
	mu      sync.Mutex
	hist    []sdk.Message
	n       int
}

func New(in io.Reader, out io.Writer, user, bot string) *Transport {
	return &Transport{In: in, Out: out, User: user, BotName: bot}
}

func (t *Transport) Name() string                         { return "local" } // the same person as the terminal chat and the web chat
func (t *Transport) Local() bool                          { return true }
func (t *Transport) Self() sdk.Identity                   { return sdk.Identity{ID: "bot", Name: t.BotName} }
func (t *Transport) Typing(context.Context, string) error { return nil }

func (t *Transport) History(_ context.Context, _ string, n int) ([]sdk.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.hist) > n {
		return append([]sdk.Message(nil), t.hist[len(t.hist)-n:]...), nil
	}
	return append([]sdk.Message(nil), t.hist...), nil
}

func (t *Transport) Send(_ context.Context, _ string, r sdk.Reply) error {
	t.mu.Lock()
	t.n++
	if r.Text != "" {
		t.hist = append(t.hist, sdk.Message{ID: fmt.Sprint("b", t.n), AuthorID: "bot", AuthorName: t.BotName, Content: r.Text, Time: time.Now()})
	}
	t.mu.Unlock()
	if r.Text != "" {
		fmt.Fprintf(t.Out, "%s: %s\n", t.BotName, r.Text)
	}
	for _, f := range r.Files {
		fmt.Fprintf(t.Out, "  [file %s, %d bytes]\n", f.Name, len(f.Data))
	}
	if len(r.Reactions) > 0 {
		fmt.Fprintf(t.Out, "  [reacts %s]\n", strings.Join(r.Reactions, " "))
	}
	return nil
}

// Run reads lines until EOF. Every line is a direct DM, so the policy answers.
func (t *Transport) Run(ctx context.Context, h sdk.Handler) error {
	sc := bufio.NewScanner(t.In)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		t.mu.Lock()
		t.n++
		m := sdk.Message{Transport: "local", ID: fmt.Sprint("u", t.n), ChannelID: "main", AuthorID: "local", AuthorName: t.User, Content: line, IsDM: true, Time: time.Now()}
		t.hist = append(t.hist, m)
		t.mu.Unlock()
		h(ctx, m)
		time.Sleep(50 * time.Millisecond)
		if ctx.Err() != nil {
			break
		}
	}
	// give the last (asynchronous) turn a moment before exiting
	time.Sleep(200 * time.Millisecond)
	return sc.Err()
}
