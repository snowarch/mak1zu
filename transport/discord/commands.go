package discord

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"github.com/snowarch/mak1zu/sdk"
)

var cmdNameRe = regexp.MustCompile(`^[-_a-z0-9]{1,32}$`)

// BuildCommands converts sdk.Commands into Discord application commands and
// checks Discord's strict schema limits before anything hits the API. Discord
// rejects the entire bulk overwrite with 400 if even one description exceeds
// 100 characters.
func BuildCommands(cmds []sdk.Command) ([]*discordgo.ApplicationCommand, error) {
	if len(cmds) > 100 {
		return nil, fmt.Errorf("too many slash commands: %d (max 100)", len(cmds))
	}
	seen := map[string]bool{}
	defs := make([]*discordgo.ApplicationCommand, 0, len(cmds))
	for _, c := range cmds {
		if !cmdNameRe.MatchString(c.Name) {
			return nil, fmt.Errorf("command %q: invalid Discord command name", c.Name)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("command %q: duplicate name", c.Name)
		}
		seen[c.Name] = true
		if n := utf8.RuneCountInString(c.Description); n < 1 || n > 100 {
			return nil, fmt.Errorf("command %q: description is %d chars (Discord requires 1-100)", c.Name, n)
		}
		if len(c.Options) > 25 {
			return nil, fmt.Errorf("command %q: too many options (%d, max 25)", c.Name, len(c.Options))
		}
		d := &discordgo.ApplicationCommand{Name: c.Name, Description: c.Description}
		sawOptional := false
		for _, o := range c.Options {
			if !cmdNameRe.MatchString(o.Name) {
				return nil, fmt.Errorf("command %q option %q: invalid name", c.Name, o.Name)
			}
			if n := utf8.RuneCountInString(o.Description); n < 1 || n > 100 {
				return nil, fmt.Errorf("command %q option %q: description is %d chars (Discord requires 1-100)", c.Name, o.Name, n)
			}
			if o.Required && sawOptional {
				return nil, fmt.Errorf("command %q option %q: required option after optional option", c.Name, o.Name)
			}
			if !o.Required {
				sawOptional = true
			}
			opt := &discordgo.ApplicationCommandOption{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        o.Name,
				Description: o.Description,
				Required:    o.Required,
			}
			for i, ch := range o.Choices {
				if i >= 25 {
					break
				}
				if n := utf8.RuneCountInString(ch); n < 1 || n > 100 {
					return nil, fmt.Errorf("command %q option %q choice %q: must be 1-100 chars", c.Name, o.Name, ch)
				}
				opt.Choices = append(opt.Choices, &discordgo.ApplicationCommandOptionChoice{Name: ch, Value: ch})
			}
			d.Options = append(d.Options, opt)
		}
		defs = append(defs, d)
	}
	return defs, nil
}

// clipReply keeps an ephemeral reply non-empty and within Discord's 2000-rune
// limit without splitting a multi-byte UTF-8 sequence.
func clipReply(out string) string {
	if strings.TrimSpace(out) == "" {
		return "(no output)"
	}
	if r := []rune(out); len(r) > 1900 {
		return string(r[:1900]) + "…"
	}
	return out
}

// RegisterCommands registers global slash commands and routes invocations to
// Command.Run. Replies are ephemeral: memory contents and errors are only for
// the person who asked.
func (t *Transport) RegisterCommands(ctx context.Context, cmds []sdk.Command) error {
	defs, err := BuildCommands(cmds)
	if err != nil {
		return err
	}
	byName := map[string]sdk.Command{}
	for _, c := range cmds {
		byName[c.Name] = c
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
		out := clipReply(cmd.Run(cctx, call))
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: out, Flags: discordgo.MessageFlagsEphemeral,
				AllowedMentions: &discordgo.MessageAllowedMentions{}},
		}); err != nil {
			slog.Warn("discord: interaction response failed", "cmd", data.Name, "err", err)
		}
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
	appID := t.appID
	if appID == "" {
		appID = t.self.ID
	}
	ow := t.overwrite
	t.mu.Unlock()
	if defs == nil || appID == "" {
		return nil
	}
	var err error
	if ow != nil {
		err = ow(appID, defs)
	} else {
		_, err = t.s.ApplicationCommandBulkOverwrite(appID, "", defs)
	}
	if err != nil {
		return err
	}
	t.mu.Lock()
	if len(t.pending) == len(defs) && (len(defs) == 0 || &t.pending[0] == &defs[0]) {
		t.pending = nil
	}
	t.mu.Unlock()
	slog.Info("discord: slash commands registered", "count", len(defs))
	return nil
}
