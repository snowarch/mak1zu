package local

import (
	"context"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

func running(t *testing.T) (*Transport, chan sdk.Message) {
	t.Helper()
	tr := New("Maki", nil)
	got := make(chan sdk.Message, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go tr.Run(ctx, func(_ context.Context, m sdk.Message) { got <- m })
	for i := 0; i < 100; i++ { // wait for the handler to be installed
		if tr.Say(ctx, "") == nil {
			<-got
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return tr, got
}

func TestSayReachesTheEngineAsAPrivateMessageFromTheLocalPerson(t *testing.T) {
	tr, got := running(t)
	tr.User = func() string { return "Ren" }
	tr.Say(context.Background(), "hello")
	m := <-got
	if m.Transport != "local" || !m.IsDM || m.AuthorID != "local" || m.AuthorName != "Ren" || m.Content != "hello" {
		t.Fatalf("%+v", m)
	}
}

func TestWhatSheSaidWhileNobodyWasHereWaitsAsBacklog(t *testing.T) {
	tr, _ := running(t)
	tr.Send(context.Background(), Channel, sdk.Reply{Text: "i was thinking about your exam"})
	ch, cancel := tr.Subscribe()
	defer cancel()
	select {
	case ev := <-ch:
		if ev.Kind != "backlog" || ev.Text != "i was thinking about your exam" {
			t.Fatalf("%+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("the message she left was lost")
	}
	// and it is delivered once
	ch2, cancel2 := tr.Subscribe()
	defer cancel2()
	select {
	case ev := <-ch2:
		t.Fatalf("backlog delivered twice: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLiveRepliesGoToEveryConnectedClientAndSlowOnesDoNotBlockHer(t *testing.T) {
	tr, _ := running(t)
	a, ca := tr.Subscribe()
	b, cb := tr.Subscribe()
	defer ca()
	defer cb()
	tr.Send(context.Background(), Channel, sdk.Reply{Text: "hi", Files: []sdk.File{{Name: "page.html", Data: []byte("<p>x")}}})
	for _, c := range []<-chan Event{a, b} {
		ev := <-c
		if ev.Kind != "reply" || ev.Text != "hi" || len(ev.Files) != 1 {
			t.Fatalf("%+v", ev)
		}
	}
	if f, ok := tr.File("page.html"); !ok || string(f.Data) != "<p>x" {
		t.Fatal("attachment not kept")
	}
	for i := 0; i < 200; i++ { // nobody reads: must never block
		tr.Typing(context.Background(), Channel)
	}
}

func TestSayBeforeTheEngineListensIsAnErrorNotAHang(t *testing.T) {
	if err := New("Maki", nil).Say(context.Background(), "hi"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestATurnSurvivesTheCallThatStartedIt(t *testing.T) {
	tr := New("Maki", nil)
	seen := make(chan error, 1)
	run, stop := context.WithCancel(context.Background())
	defer stop()
	go tr.Run(run, func(ctx context.Context, m sdk.Message) {
		time.Sleep(50 * time.Millisecond) // the model call takes a while
		seen <- ctx.Err()
	})
	for i := 0; i < 100; i++ {
		req, cancel := context.WithCancel(context.Background())
		if tr.Say(req, "hi") == nil {
			cancel() // the HTTP request is over
			break
		}
		cancel()
		time.Sleep(5 * time.Millisecond)
	}
	if err := <-seen; err != nil {
		t.Fatalf("the turn was cancelled with the request: %v", err)
	}
}
