package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/transport/local"
	"github.com/snowarch/mak1zu/tui"
)

// newLocal builds the local conversation transport: its history is the turns
// table (so it survives restarts), and the person at the keyboard is called
// whatever they asked to be called, else their account name on this machine.
func newLocal(st *config.Store, mem *memory.Store) *local.Transport {
	botName := "Maki"
	if pa, err := (persona.Library{Dir: st.Abs(st.Get().Persona.Dir)}).Load(st.Get().Persona.Active); err == nil {
		botName = pa.Name
	}
	lt := local.New(botName, nil)
	lt.User = func() string {
		if p, ok, _ := mem.Lookup(context.Background(), "local", "local"); ok && p.Display() != "" {
			return p.Display()
		}
		if u, err := user.Current(); err == nil && u.Username != "" {
			return u.Username
		}
		return "you"
	}
	self := lt.Self().ID
	lt.SetHistory(func(ctx context.Context, n int) []sdk.Message {
		turns, err := mem.RecentTurns(ctx, local.Channel, n)
		if err != nil {
			return nil
		}
		var out []sdk.Message
		for _, t := range turns {
			if t.UserMsg != "" {
				out = append(out, sdk.Message{Transport: "local", ChannelID: local.Channel, AuthorID: "local", AuthorName: lt.User(), Content: t.UserMsg})
			}
			if t.Reply != "" {
				out = append(out, sdk.Message{Transport: "local", ChannelID: local.Channel, AuthorID: self, AuthorName: botName, Content: t.Reply})
			}
		}
		return out
	})
	return lt
}

// cmdTUI opens the terminal chat: on a running daemon if there is one, else on
// an engine of its own (Discord stays off there: `mak1zu run` owns Discord).
func cmdTUI(st *config.Store) error {
	if !isTerminal(os.Stdin) {
		return errors.New("the terminal chat needs a terminal; use `mak1zu chat` in a pipe")
	}
	cfg := st.Get()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	herName := "Maki"
	if pa, err := (persona.Library{Dir: st.Abs(cfg.Persona.Dir)}).Load(cfg.Persona.Active); err == nil {
		herName = pa.Name
	}
	if cfg.WebUI.Enabled {
		host := cfg.WebUI.Host
		if host == "" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		r := &tui.Remote{Base: fmt.Sprintf("http://%s:%d", host, cfg.WebUI.Port), Token: cfg.WebUI.Token}
		if r.Probe(ctx) {
			return tui.Run(ctx, r, herName)
		}
	}

	unlock, err := singleInstance(filepath.Dir(st.Abs(cfg.Memory.Path)))
	if err != nil {
		return fmt.Errorf("she is already running somewhere I cannot reach (%v). If the web panel is off or on another port, turn it on in .makizu/config.json (web_ui) so the terminal can attach", err)
	}
	defer unlock()
	e, mem, _, err := build(st, idle{})
	if err != nil {
		return err
	}
	defer mem.Close()
	lt := newLocal(st, mem)
	e.Add(lt)
	// the engine's own log would scribble over the screen
	e.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	go func() { _ = e.Run(ctx) }()
	return tui.Run(ctx, &tui.Embedded{T: lt, Detail: "running on its own: `mak1zu run` keeps her on Discord",
		Run: func(ctx context.Context, name, arg string) (string, bool) {
			return e.RunCommand(ctx, "local", "local", lt.User(), name, arg)
		}}, herName)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
