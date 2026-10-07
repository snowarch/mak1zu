// Package discord adapts discordgo to sdk.Transport. It only translates:
// whether to answer is the engine policy's job. Two platform rules live
// here because only the platform can enforce them: custom emoji IDs are
// resolved from live guild state at send time, and IDs stay strings.
package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/snowarch/mak1zu/sdk"
)

type Transport struct {
	s       *discordgo.Session
	self    sdk.Identity
	mu      sync.RWMutex
	chNames map[string]string
	pending []*discordgo.ApplicationCommand
}

func New(token string) (*Transport, error) {
	if token == "" {
		return nil, errors.New("discord token missing (set the env var named in discord.token_env)")
	}
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	s.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages |
		discordgo.IntentsMessageContent | discordgo.IntentsGuildEmojis | discordgo.IntentsGuildMessageReactions
	s.StateEnabled = true
	return &Transport{s: s, chNames: map[string]string{}}, nil
}

func (t *Transport) Name() string       { return "discord" }
func (t *Transport) Self() sdk.Identity { t.mu.RLock(); defer t.mu.RUnlock(); return t.self }

func (t *Transport) Run(ctx context.Context, h sdk.Handler) error {
	t.s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		t.mu.Lock()
		t.self = sdk.Identity{ID: r.User.ID, Name: r.User.Username}
		t.mu.Unlock()
		_ = t.flushCommands()
	})
	t.s.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		if m.Author == nil {
			return
		}
		h(ctx, t.convert(m.Message))
	})
	if err := t.s.Open(); err != nil {
		return err
	}
	<-ctx.Done()
	return t.s.Close()
}

func (t *Transport) convert(m *discordgo.Message) sdk.Message {
	self := t.Self()
	out := sdk.Message{
		Transport: "discord", ID: m.ID, ChannelID: m.ChannelID, GuildID: m.GuildID, AuthorID: m.Author.ID, AuthorName: displayName(m),
		Content: t.renderMentions(m), IsDM: m.GuildID == "", IsBot: m.Author.Bot, WebhookID: m.WebhookID, Time: m.Timestamp,
	}
	for _, u := range m.Mentions {
		if u.ID == self.ID {
			out.Mentioned = true
		}
	}
	if m.ReferencedMessage != nil {
		out.ReplyToID = m.ReferencedMessage.ID
		out.ReplyToBot = m.ReferencedMessage.Author != nil && m.ReferencedMessage.Author.ID == self.ID
	}
	for _, a := range m.Attachments {
		out.Attachments = append(out.Attachments, sdk.Attachment{Name: a.Filename, URL: a.URL, ContentType: a.ContentType, Size: int64(a.Size)})
	}
	if ch, err := t.s.State.Channel(m.ChannelID); err == nil {
		out.ChannelName = ch.Name
	}
	if g, err := t.s.State.Guild(m.GuildID); err == nil && m.GuildID != "" {
		out.GuildName = g.Name
	}
	return out
}

func displayName(m *discordgo.Message) string {
	if m.Member != nil && m.Member.Nick != "" {
		return m.Member.Nick
	}
	if m.Author.GlobalName != "" {
		return m.Author.GlobalName
	}
	return m.Author.Username
}

var mentionRe = regexp.MustCompile(`<@!?(\d+)>`)

// renderMentions turns <@id> into @name so the model reads names, not IDs.
func (t *Transport) renderMentions(m *discordgo.Message) string {
	names := map[string]string{}
	for _, u := range m.Mentions {
		names[u.ID] = u.Username
	}
	return mentionRe.ReplaceAllStringFunc(m.Content, func(s string) string {
		id := mentionRe.FindStringSubmatch(s)[1]
		if n, ok := names[id]; ok {
			return "@" + n
		}
		return "@someone"
	})
}

func (t *Transport) Typing(_ context.Context, ch string) error { return t.s.ChannelTyping(ch) }

func (t *Transport) History(_ context.Context, ch string, n int) ([]sdk.Message, error) {
	if n > 100 {
		n = 100
	}
	ms, err := t.s.ChannelMessages(ch, n, "", "", "")
	if err != nil {
		return nil, err
	}
	out := make([]sdk.Message, 0, len(ms))
	for i := len(ms) - 1; i >= 0; i-- { // API returns newest first
		if ms[i].Author != nil {
			out = append(out, t.convert(ms[i]))
		}
	}
	return out, nil
}

var emojiTok = regexp.MustCompile(`:([a-zA-Z0-9_]{2,32}):`)

// EmojiNames lists the guild's custom emoji names (never IDs).
func (t *Transport) EmojiNames(guildID string) []string {
	g, err := t.s.State.Guild(guildID)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range g.Emojis {
		if e.Available {
			out = append(out, ":"+e.Name+":")
		}
		if len(out) >= 40 {
			break
		}
	}
	return out
}

// resolveEmoji rewrites :name: into <:name:id> using live guild state and
// removes tokens that do not exist. The model's text is untrusted.
func (t *Transport) resolveEmoji(guildID, text string) string {
	g, err := t.s.State.Guild(guildID)
	if err != nil || guildID == "" {
		return text
	}
	byName := map[string]*discordgo.Emoji{}
	for _, e := range g.Emojis {
		byName[strings.ToLower(e.Name)] = e
	}
	return emojiTok.ReplaceAllStringFunc(text, func(tok string) string {
		name := emojiTok.FindStringSubmatch(tok)[1]
		if e, ok := byName[strings.ToLower(name)]; ok && e.Available {
			if e.Animated {
				return fmt.Sprintf("<a:%s:%s>", e.Name, e.ID)
			}
			return fmt.Sprintf("<:%s:%s>", e.Name, e.ID)
		}
		if isShortcodeLikeTime(tok) {
			return tok // 10:30:15 style timestamps are not emoji
		}
		return ""
	})
}

func isShortcodeLikeTime(tok string) bool { return strings.Trim(tok, ":0123456789") == "" }

func (t *Transport) Send(ctx context.Context, ch string, r sdk.Reply) error {
	if r.Text == "" && len(r.Files) == 0 {
		for _, e := range r.Reactions {
			if r.ReplyToID != "" {
				_ = t.s.MessageReactionAdd(ch, r.ReplyToID, e)
			}
		}
		return nil
	}
	gid := ""
	if c, err := t.s.State.Channel(ch); err == nil {
		gid = c.GuildID
	}
	// Never let model-generated text ping everyone or arbitrary roles.
	msg := &discordgo.MessageSend{Content: t.resolveEmoji(gid, r.Text),
		AllowedMentions: &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{discordgo.AllowedMentionTypeUsers}}}
	if r.ReplyToID != "" {
		msg.Reference = &discordgo.MessageReference{MessageID: r.ReplyToID, ChannelID: ch, GuildID: gid}
	}
	for _, f := range r.Files {
		msg.Files = append(msg.Files, &discordgo.File{Name: f.Name, Reader: bytes.NewReader(f.Data)})
	}
	if _, err := t.s.ChannelMessageSendComplex(ch, msg, discordgo.WithContext(ctx)); err != nil {
		return err
	}
	for _, e := range r.Reactions {
		if r.ReplyToID != "" {
			_ = t.s.MessageReactionAdd(ch, r.ReplyToID, e)
		}
	}
	return nil
}

// Unseen implements sdk.Catchup: human messages since her last message.
func (t *Transport) Unseen(ctx context.Context, ch string, limit int) ([]sdk.Message, error) {
	h, err := t.History(ctx, ch, 50)
	if err != nil {
		return nil, err
	}
	self := t.Self().ID
	last := -1
	for i, m := range h {
		if m.AuthorID == self {
			last = i
		}
	}
	var out []sdk.Message
	for _, m := range h[last+1:] {
		if !m.IsBot && time.Since(m.Time) < 6*time.Hour {
			out = append(out, m)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
