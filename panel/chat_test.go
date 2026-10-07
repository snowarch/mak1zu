package panel

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/transport/local"
	"github.com/snowarch/mak1zu/tui"
)

// The terminal UI's HTTP client against the real panel handlers: what the
// daemon serves is what the client expects.
func TestTerminalClientAgainstTheRealPanel(t *testing.T) {
	s, _ := newServer(t)
	lt := local.New("Maki", func(context.Context, int) []sdk.Message {
		return []sdk.Message{{AuthorID: "local", Content: "hello"}, {AuthorID: "maki-local", Content: "hi"}}
	})
	got := make(chan sdk.Message, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go lt.Run(ctx, func(_ context.Context, m sdk.Message) { got <- m })
	for i := 0; i < 100 && lt.Say(ctx, "") != nil; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	<-got
	s.Chat = lt
	s.Command = func(_ context.Context, name, arg string) (string, bool) {
		if name == "ping" {
			return "pong " + arg, true
		}
		return "", false
	}
	ts := httptest.NewServer(s.Handler())
	defer func() { cancel(); ts.Close() }() // the stream must end first or Close waits for it forever

	r := &tui.Remote{Base: ts.URL}
	if !r.Probe(ctx) {
		t.Fatal("a real daemon was not recognised")
	}
	h, err := r.History(ctx, 10)
	if err != nil || len(h) != 2 || h[0].Who != "you" || h[1].Who != "her" || h[1].Text != "hi" {
		t.Fatalf("history: %v %v", h, err)
	}
	if err := r.Say(ctx, "ping me"); err != nil {
		t.Fatal(err)
	}
	if m := <-got; m.Content != "ping me" {
		t.Fatalf("%+v", m)
	}
	evs, err := r.Stream(ctx)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // let the subscription register before she speaks
	lt.Send(ctx, local.Channel, sdk.Reply{Text: "pong from her"})
	select {
	case ev := <-evs:
		if ev.Kind != "reply" || ev.Text != "pong from her" {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event over the stream")
	}
	if out, ok, err := r.Command(ctx, "ping", "x"); err != nil || !ok || out != "pong x" {
		t.Fatalf("%q %v %v", out, ok, err)
	}
	if _, ok, _ := r.Command(ctx, "nope", ""); ok {
		t.Fatal("unknown command reported as known")
	}
	// a token-protected daemon refuses a client without it
	st := s.Cfg.Get()
	st.WebUI.Token = "sekret"
	s.Cfg = config.New(s.Cfg.Path(), st)
	if (&tui.Remote{Base: ts.URL}).Probe(ctx) {
		t.Fatal("probe succeeded without the token")
	}
	if !(&tui.Remote{Base: ts.URL, Token: "sekret"}).Probe(ctx) {
		t.Fatal("probe failed with the token")
	}
}
