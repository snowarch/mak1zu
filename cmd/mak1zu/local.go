package main

import (
	"context"
	"os/user"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/transport/local"
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
