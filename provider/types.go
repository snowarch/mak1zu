// Package provider talks to LLM backends. Any OpenAI-compatible endpoint
// works ("chat" protocol) and models that only speak /responses are
// supported ("responses" protocol). Adapters translate capabilities only:
// they know nothing about personas, memory or Discord.
package provider

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Role string

const (
	System    Role = "system"
	User      Role = "user"
	Assistant Role = "assistant"
	ToolRole  Role = "tool"
)

type ToolCall struct {
	ID   string
	Name string
	Args string // raw JSON
}

type Message struct {
	Role       Role
	Content    string
	Name       string   // speaker label for multi-user history
	Images     []string // URLs or data: URLs
	ToolCalls  []ToolCall
	ToolCallID string
}

type ToolDef struct {
	Name        string
	Description string
	Schema      []byte // JSON Schema object
}

type Request struct {
	System      string
	Messages    []Message
	Tools       []ToolDef
	MaxTokens   int
	Temperature *float64
}

func (r Request) HasImages() bool {
	for _, m := range r.Messages {
		if len(m.Images) > 0 {
			return true
		}
	}
	return false
}

type Response struct {
	Text      string
	ToolCalls []ToolCall
	Provider  string
	Model     string
	Latency   time.Duration
}

type Client interface {
	Complete(ctx context.Context, r Request) (Response, error)
}

// Kind classifies why a call failed so the router and the incident layer can
// react differently to a dead provider, a quota wall and a bad configuration.
type Kind int

const (
	KindUnknown Kind = iota
	KindTimeout
	KindRateLimit
	KindAuth // bad or missing key: configuration, not an outage
	KindServer
	KindEmpty // 200 OK but nothing visible (reasoning ate the budget)
	KindBadRequest
	KindUnsupported // capability the route does not have (vision, tools)
	KindCanceled
)

func (k Kind) String() string {
	return [...]string{"unknown", "timeout", "rate_limit", "auth", "server", "empty", "bad_request", "unsupported", "canceled"}[k]
}

type Error struct {
	Kind     Kind
	Provider string
	Status   int
	Msg      string
}

func (e *Error) Error() string {
	return fmt.Sprintf("provider %s: %s (status %d): %s", e.Provider, e.Kind, e.Status, e.Msg)
}

// Transient reports whether retrying the same turn once is reasonable.
func (e *Error) Transient() bool {
	switch e.Kind {
	case KindTimeout, KindServer, KindEmpty, KindRateLimit:
		return true
	}
	return false
}

func KindOf(err error) Kind {
	var pe *Error
	if errors.As(err, &pe) {
		return pe.Kind
	}
	if errors.Is(err, context.Canceled) {
		return KindCanceled
	}
	return KindUnknown
}
