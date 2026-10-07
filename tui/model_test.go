package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/snowarch/mak1zu/transport/local"
)

type fakeConn struct {
	said []string
	cmds []string
	ch   chan local.Event
}

func (f *fakeConn) Say(_ context.Context, t string) error { f.said = append(f.said, t); return nil }
func (f *fakeConn) Stream(context.Context) (<-chan local.Event, error) {
	return f.ch, nil
}
func (f *fakeConn) History(context.Context, int) ([]Line, error) {
	return []Line{{Who: "you", Text: "earlier hello"}, {Who: "her", Text: "earlier hi"}}, nil
}
func (f *fakeConn) Command(_ context.Context, n, a string) (string, bool, error) {
	f.cmds = append(f.cmds, n+" "+a)
	if n == "memories" {
		return "#1 Ren likes Frieren (today)", true, nil
	}
	return "", false, nil
}
func (f *fakeConn) Where() string { return "test" }

// drive feeds a message and runs the commands it returns that finish quickly.
func drive(m Model, msg tea.Msg) Model {
	next, cmd := m.Update(msg)
	m = next.(Model)
	if cmd != nil {
		done := make(chan tea.Msg, 1)
		go func() { done <- cmd() }()
		select {
		case out := <-done:
			switch out.(type) {
			case tea.BatchMsg, tea.QuitMsg: // batches carry timers and blinks; not what these tests are about
			default:
				if out != nil {
					next, _ = m.Update(out)
					m = next.(Model)
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	return m
}

func screen(m Model) string { return m.View().Content }

func ready(t *testing.T) (Model, *fakeConn) {
	t.Helper()
	f := &fakeConn{ch: make(chan local.Event, 8)}
	m := New(context.Background(), f, "Maki")
	m = drive(m, tea.WindowSizeMsg{Width: 70, Height: 20})
	m = drive(m, historyMsg{{Who: "you", Text: "earlier hello"}, {Who: "her", Text: "earlier hi"}})
	return m, f
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = drive(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func TestHistoryAndNameShowOnOpen(t *testing.T) {
	m, _ := ready(t)
	s := screen(m)
	for _, want := range []string{"mak1zu", "maki", "earlier hello", "earlier hi", "test"} {
		if !strings.Contains(s, want) {
			t.Fatalf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestSendingAMessageShowsItAndWaitsForHer(t *testing.T) {
	m, f := ready(t)
	m = typeText(m, "hello there")
	m = drive(m, press("enter"))
	if len(f.said) != 1 || f.said[0] != "hello there" {
		t.Fatalf("said %v", f.said)
	}
	s := screen(m)
	if !strings.Contains(s, "hello there") || !strings.Contains(s, "is typing") {
		t.Fatalf("a sent message should show with a typing hint:\n%s", s)
	}
	m = drive(m, eventMsg(local.Event{Kind: "reply", Text: "hey. you again."}))
	s = screen(m)
	if !strings.Contains(s, "hey. you again.") || strings.Contains(s, "is typing") {
		t.Fatalf("her reply should replace the typing hint:\n%s", s)
	}
}

func TestWhatSheSaidWhileYouWereAwayIsMarked(t *testing.T) {
	m, _ := ready(t)
	m = drive(m, eventMsg(local.Event{Kind: "backlog", Text: "how did the exam go"}))
	m = drive(m, eventMsg(local.Event{Kind: "backlog", Text: "and the cat?"}))
	s := screen(m)
	if strings.Count(s, "while you were away") != 1 || !strings.Contains(s, "how did the exam go") || !strings.Contains(s, "and the cat?") {
		t.Fatalf("backlog:\n%s", s)
	}
}

func TestSlashCommandsGoToTheConnectionAndNeverToHer(t *testing.T) {
	m, f := ready(t)
	m = typeText(m, "/memories")
	m = drive(m, press("enter"))
	if len(f.said) != 0 || len(f.cmds) != 1 || f.cmds[0] != "memories " {
		t.Fatalf("said=%v cmds=%v", f.said, f.cmds)
	}
	if !strings.Contains(screen(m), "Ren likes Frieren") {
		t.Fatalf("command output missing:\n%s", screen(m))
	}
	m = typeText(m, "/nonsense")
	m = drive(m, press("enter"))
	if !strings.Contains(screen(m), "no such command") {
		t.Fatalf("unknown command should say so:\n%s", screen(m))
	}
	m = typeText(m, "/help")
	m = drive(m, press("enter"))
	if !strings.Contains(screen(m), "/diary") {
		t.Fatal("/help should list commands")
	}
}

func TestEmptyInputSendsNothingAndTheWindowCanBeTiny(t *testing.T) {
	m, f := ready(t)
	m = drive(m, press("enter"))
	if len(f.said) != 0 {
		t.Fatal("an empty line was sent")
	}
	m = drive(m, tea.WindowSizeMsg{Width: 12, Height: 5})
	_ = screen(m) // must not panic on a tiny window
}
