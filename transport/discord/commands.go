package discord

import (
	"context"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/snowarch/mak1zu/sdk"
)

// RegisterCommands registers global slash commands and routes invocations to
// Command.Run. Replies are ephemeral: memory contents and errors are only for
// the person who asked.
func (t *Transport) RegisterCommands(ctx context.Context, cmds []sdk.Command) error {
	byName := map[string]sdk.Command{}
	var defs []*discordgo.ApplicationCommand
	for _, c := range cmds {
		byName[c.Name] = c
		d := &discordgo.ApplicationCommand{Name: c.Name, Description: c.Description}
		for _, o := range c.Options {
			opt := &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: o.Name, Description: o.Description, Required: o.Required}
			for i, ch := range o.Choices {
				if i >= 25 {
					break
				}
				opt.Choices = append(opt.Choices, &discordgo.ApplicationCommandOptionChoice{Name: ch, Value: ch})
			}
			d.Options = append(d.Options, opt)
		}
		defs = append(defs, d)
	}
	t.s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		data := i.ApplicationCommandData()
		cmd, ok := byName[data.Name]
		if !ok {
			return
		}
		u := i.User
		if i.Member != nil {
			u = i.Member.User
		}
		if u == nil {
			return
		}
		call := sdk.CommandCall{Transport: "discord", UserID: u.ID, UserName: u.Username, ChannelID: i.ChannelID, Args: map[string]string{}}
		for _, o := range data.Options {
			call.Args[o.Name] = o.StringValue()
		}
		cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
		defer cancel()
		out := cmd.Run(cctx, call)
		if len(out) > 1900 {
			out = out[:1900] + "…"
		}
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: out, Flags: discordgo.MessageFlagsEphemeral,
				AllowedMentions: &discordgo.MessageAllowedMentions{}},
		})
	})
	t.mu.Lock()
	t.pending = defs
	t.mu.Unlock()
	if t.Self().ID != "" {
		return t.flushCommands()
	}
	return nil // Run's Ready handler flushes once the identity is known
}

func (t *Transport) flushCommands() error {
	t.mu.Lock()
	defs := t.pending
	t.pending = nil
	self := t.self.ID
	t.mu.Unlock()
	if defs == nil || self == "" {
		return nil
	}
	_, err := t.s.ApplicationCommandBulkOverwrite(self, "", defs)
	return err
}
