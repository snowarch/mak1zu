// Package sdk is the public, stable surface of Mak1zu. Everything a plugin,
// transport or tool author needs lives here and depends on nothing but the
// standard library.
package sdk

import (
	"context"
	"encoding/json"
	"time"
)

// Attachment is a file the user sent with a message.
type Attachment struct {
	Name        string
	URL         string
	ContentType string
	Size        int64
}

// Message is a transport-neutral inbound chat message.
type Message struct {
	ID          string
	ChannelID   string
	ChannelName string
	GuildID     string // empty for DMs
	GuildName   string
	AuthorID    string
	AuthorName  string
	Content     string

	IsDM        bool
	IsBot       bool
	WebhookID   string // set for webhook-posted messages (bridges, test harnesses)
	Mentioned   bool   // a real platform mention of the companion
	ReplyToID   string // message being replied to, if any
	ReplyToBot  bool   // the replied-to message was written by the companion
	Attachments []Attachment
	Time        time.Time
}

// File is an outbound attachment.
type File struct {
	Name string
	Data []byte
}

// Reply is what the engine hands to a transport.
type Reply struct {
	Text      string
	Files     []File
	Reactions []string
	ReplyToID string
}

// Handler is called by a transport for every inbound message.
type Handler func(ctx context.Context, m Message)

// Transport connects the engine to a chat platform. Discord is one
// implementation; the terminal is another. A transport never decides whether
// the companion should talk: it only delivers events and sends replies.
type Transport interface {
	Name() string
	Run(ctx context.Context, h Handler) error
	Send(ctx context.Context, channelID string, r Reply) error
	Typing(ctx context.Context, channelID string) error
	// History returns up to n recent messages, oldest first.
	History(ctx context.Context, channelID string, n int) ([]Message, error)
	// Self returns the companion's identity on this platform.
	Self() Identity
}

// Local is an optional transport capability: the person on the other end is
// whoever runs the machine (the terminal, a local chat window). They become the
// owner on first contact when nobody holds that role yet.
type Local interface{ Local() bool }

// Identity names the companion on a platform.
type Identity struct {
	ID   string
	Name string
}

// ToolSpec describes a tool to the model. Schema is a JSON Schema object.
type ToolSpec struct {
	Name        string
	Description string
	Schema      json.RawMessage
	// Heavy marks a slow or expensive tool: once one runs, the turn gets the
	// larger token budget. Every tool is offered on every turn.
	Heavy bool
}

// CallEnv is the context a tool runs in. The speaker is always the human who
// triggered the turn: tools must never leak one person's private memory to
// another.
type CallEnv struct {
	Speaker   Identity
	ChannelID string
	GuildID   string
	IsDM      bool
	Persona   string
	// QueueFile attaches a file to the final reply.
	QueueFile func(File)
	// React adds a reaction to the triggering message.
	React func(emoji string)
	// AttachLink queues a link (a GIF) sent as its own message after the text,
	// so media never delays or replaces the spoken reply.
	AttachLink func(url string)
}

// Tool is something the companion can do. Tool results are data, never
// instructions: the engine wraps them so the model treats them that way.
type Tool interface {
	Spec() ToolSpec
	Call(ctx context.Context, args json.RawMessage, env *CallEnv) (string, error)
}

// ToolFunc adapts a function to Tool.
type ToolFunc struct {
	S ToolSpec
	F func(ctx context.Context, args json.RawMessage, env *CallEnv) (string, error)
}

func (t ToolFunc) Spec() ToolSpec { return t.S }
func (t ToolFunc) Call(ctx context.Context, a json.RawMessage, e *CallEnv) (string, error) {
	return t.F(ctx, a, e)
}

// Hooks let plugins observe or reshape a turn. Every field is optional.
type Hooks struct {
	// BeforeReply may return extra system context for this turn.
	BeforeReply func(ctx context.Context, m Message) string
	// AfterReply sees the final public text.
	AfterReply func(ctx context.Context, m Message, final string)
	// FilterOutput can rewrite or veto (return "") the final text.
	FilterOutput func(text string) string
}

// Plugin bundles tools and hooks. Register with engine.Use.
type Plugin interface {
	Name() string
	Tools() []Tool
	Hooks() Hooks
}

// EmojiProvider is an optional transport capability. The engine shows the
// model semantic emoji names; the platform is the authority on IDs and
// resolves ":name:" to its real syntax at send time (dropping unknown ones).
type EmojiProvider interface {
	EmojiNames(guildID string) []string
}

// Presence is an optional transport capability for the "owner is away" and
// startup catch-up paths.
type Catchup interface {
	// Unseen returns human messages in channelID newer than the companion's
	// last message, oldest first.
	Unseen(ctx context.Context, channelID string, limit int) ([]Message, error)
}

// CommandOption is one argument of a slash command.
type CommandOption struct {
	Name        string
	Description string
	Required    bool
	Choices     []string // optional fixed choices
}

// CommandCall is an invocation of a platform command.
type CommandCall struct {
	UserID    string // the platform account; the engine maps it to a person
	UserName  string
	ChannelID string
	Args      map[string]string
}

// Command is a platform-level command (Discord slash command). Run returns
// the (ephemeral) text shown to the caller. OwnerOnly commands are rejected
// by the engine before Run.
type Command struct {
	Name        string
	Description string
	Options     []CommandOption
	OwnerOnly   bool
	Run         func(ctx context.Context, c CommandCall) string
}

// CommandHost is an optional transport capability: register commands and
// route invocations back to Command.Run.
type CommandHost interface {
	RegisterCommands(ctx context.Context, cmds []Command) error
}
