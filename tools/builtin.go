package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/sdk"
)

// Deps are the engine services built-in tools may use.
type Deps struct {
	Mem        *memory.Store
	Persona    func() string
	SearxURL   func() string // optional SearXNG base URL for web_search
	SelfReview func(channelID string) string
}

func args[T any](raw json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

// Builtins returns the standard toolbox.
func Builtins(d Deps) []sdk.Tool {
	t := []sdk.Tool{
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "now", Description: "Current date and time (UTC and local).", Schema: Schema(nil, nil)},
			F: func(ctx context.Context, _ json.RawMessage, _ *sdk.CallEnv) (string, error) {
				n := time.Now()
				return n.Format(time.RFC1123) + " / " + n.UTC().Format(time.RFC3339), nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "remember", Description: "Save something worth remembering about the person you are talking to (a stable fact, preference, or something that happened). Not for chit-chat.",
			Schema: Schema([]string{"content"}, map[string][2]string{"content": {"string", "what to remember, one sentence"}, "kind": {"string", "semantic (fact) or episodic (event)"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Content, Kind string }](raw)
				if err != nil {
					return "", err
				}
				k := memory.Semantic
				if a.Kind == "episodic" {
					k = memory.Episodic
				}
				if !WorthKeeping(a.Content) {
					return "not saved: too vague or too short to be useful", nil
				}
				id, err := d.Mem.RememberFrom(ctx, d.Persona(), k, env.Speaker.ID, env.Transport, a.Content, 0.6, "")
				return fmt.Sprintf("saved #%d", id), err
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "set_profile", Description: "Save how the person you are talking to wants to be treated: what to call them, their pronouns, language, time zone, whether you may start conversations, quiet hours. Only what they told you about themselves, on every platform they use. An empty value clears it. If they ask you to stop checking in on them, set checkins off.",
			Schema: Schema([]string{"field", "value"}, map[string][2]string{"field": {"string", "call_me, pronouns, language, tz, checkins (on/off: whether you may start conversations) or quiet (hours you must not message them, 23:00-08:00)"}, "value": {"string", "the value, e.g. Ren, they/them, Spanish, Europe/Madrid, off, 23:00-08:00"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Field, Value string }](raw)
				if err != nil {
					return "", err
				}
				if err := d.Mem.SetProfile(ctx, env.Speaker.ID, a.Field, a.Value); err != nil {
					return "not saved: " + err.Error(), nil
				}
				return "saved", nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "open_thread", Description: "Note something still in flight in the person's life (an exam, a sick pet, a decision they are stuck on) so you can follow up later. Not for facts that are simply true.",
			Schema: Schema([]string{"text"}, map[string][2]string{"text": {"string", "one short sentence"}, "due": {"string", "when it happens or is due, RFC3339 or YYYY-MM-DD, if known"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Text, Due string }](raw)
				if err != nil {
					return "", err
				}
				due, _ := ParseDue(a.Due)
				id, err := d.Mem.AddThread(ctx, env.Speaker.ID, env.Transport, a.Text, due)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("thread #%d open", id), nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "close_thread", Description: "Close a thread that is over (it happened, it resolved, they dropped it).",
			Schema: Schema([]string{"id"}, map[string][2]string{"id": {"integer", "the thread number from <open_threads>"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ ID int64 }](raw)
				if err != nil {
					return "", err
				}
				if ok, _ := d.Mem.CloseThread(ctx, env.Speaker.ID, a.ID); !ok {
					return "no such open thread", nil
				}
				return "closed", nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "note_bit", Description: "Save a running bit: a joke, nickname or reference only the two of you share, worth calling back to later. Be sparing; most conversations produce none.",
			Schema: Schema([]string{"text", "trigger"}, map[string][2]string{"text": {"string", "what the bit is and where it came from, one sentence"}, "trigger": {"string", "a short word or phrase that will appear in your reply when you call it back"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Text, Trigger string }](raw)
				if err != nil {
					return "", err
				}
				if _, err := d.Mem.AddBit(ctx, d.Persona(), env.Speaker.ID, env.Transport, a.Text, a.Trigger); err != nil {
					return "", err
				}
				return "noted", nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "recall", Description: "Search your memories about the person you are talking to.",
			Schema: Schema([]string{"query"}, map[string][2]string{"query": {"string", "what to look for"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Query string }](raw)
				if err != nil {
					return "", err
				}
				ms, err := d.Mem.Recall(ctx, d.Persona(), env.Speaker.ID, a.Query, 6)
				if err != nil || len(ms) == 0 {
					return "nothing found", err
				}
				var b strings.Builder
				for _, m := range ms {
					fmt.Fprintf(&b, "- %s\n", m.Content)
				}
				return b.String(), nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "forget_me", Description: "Erase everything stored about the person you are talking to. Only when they clearly ask to be forgotten.",
			Schema: Schema([]string{"confirm"}, map[string][2]string{"confirm": {"boolean", "true if they explicitly asked"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Confirm bool }](raw)
				if err != nil || !a.Confirm {
					return "not confirmed; nothing erased", err
				}
				return "erased", d.Mem.ForgetUser(ctx, env.Speaker.ID)
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "set_reminder", Description: "Remind the person later. Use `in` like 45m, 2h, 1d (or `at` as RFC3339). Ask when they didn't say when.",
			Schema: Schema([]string{"content"}, map[string][2]string{"content": {"string", "what to remind about"}, "in": {"string", "duration like 90m, 2h, 1d"}, "at": {"string", "RFC3339 timestamp"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Content, In, At string }](raw)
				if err != nil {
					return "", err
				}
				due, err := ParseWhen(a.In, a.At, time.Now())
				if err != nil {
					return "", err
				}
				if _, err := d.Mem.AddReminder(ctx, env.Speaker.ID, env.ChannelID, a.Content, due); err != nil {
					return "", err
				}
				return "reminder set for " + due.Local().Format("Mon 2 Jan 15:04"), nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "react", Description: "React to the message you are answering with an emoji instead of (or besides) words.",
			Schema: Schema([]string{"emoji"}, map[string][2]string{"emoji": {"string", "a unicode emoji"}})},
			F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
				a, err := args[struct{ Emoji string }](raw)
				if err != nil || a.Emoji == "" {
					return "", errors.New("emoji required")
				}
				if env.React != nil {
					env.React(a.Emoji)
				}
				return "reacted", nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "read_url", Heavy: true, Description: "Fetch a public web page and return its readable text. Only public http(s) URLs.",
			Schema: Schema([]string{"url"}, map[string][2]string{"url": {"string", "the page"}})},
			F: func(ctx context.Context, raw json.RawMessage, _ *sdk.CallEnv) (string, error) {
				a, err := args[struct{ URL string }](raw)
				if err != nil {
					return "", err
				}
				b, ct, err := Fetch(ctx, a.URL, 1<<20)
				if err != nil {
					return "", err
				}
				if strings.Contains(ct, "html") || strings.Contains(string(b[:min(len(b), 200)]), "<") {
					return HTMLToText(string(b)), nil
				}
				return string(b), nil
			}},
	}
	if d.SelfReview != nil {
		t = append(t, sdk.ToolFunc{S: sdk.ToolSpec{Name: "review_myself", Description: "Look at your own recent replies, habits and unresolved failures in this channel. Use when someone says you are repeating yourself, broke, or promised something.", Schema: Schema(nil, nil)},
			F: func(_ context.Context, _ json.RawMessage, env *sdk.CallEnv) (string, error) {
				return d.SelfReview(env.ChannelID), nil
			}})
	}
	t = append(t, WebSearch(d.SearxURL))
	return t
}

var vague = regexp.MustCompile(`(?i)^(ok|okay|yes|no|lol|haha|thanks|hi|hello|hey|idk|nothing|nvm)\W*$`)

// WorthKeeping rejects memory candidates that would only add noise.
func WorthKeeping(s string) bool {
	s = strings.TrimSpace(s)
	return len([]rune(s)) >= 12 && !vague.MatchString(s) && len(strings.Fields(s)) >= 3
}

var durRe = regexp.MustCompile(`(?i)(\d+)\s*(d|h|m|min|s)`)

// ParseWhen accepts `in` (45m, 2h30m, 1d) or `at` (RFC3339).
func ParseWhen(in, at string, now time.Time) (time.Time, error) {
	if at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return t, fmt.Errorf("bad `at` timestamp: %w", err)
		}
		if t.Before(now.Add(-time.Minute)) {
			return t, errors.New("that time is in the past")
		}
		return t, nil
	}
	ms := durRe.FindAllStringSubmatch(in, -1)
	if len(ms) == 0 {
		return time.Time{}, errors.New("need `in` like 45m/2h/1d or `at` as RFC3339")
	}
	var d time.Duration
	for _, m := range ms {
		n, _ := strconv.Atoi(m[1])
		switch strings.ToLower(m[2]) {
		case "d":
			d += time.Duration(n) * 24 * time.Hour
		case "h":
			d += time.Duration(n) * time.Hour
		case "m", "min":
			d += time.Duration(n) * time.Minute
		case "s":
			d += time.Duration(n) * time.Second
		}
	}
	if d <= 0 || d > 366*24*time.Hour {
		return time.Time{}, errors.New("duration out of range")
	}
	return now.Add(d), nil
}

var (
	scriptRe = regexp.MustCompile(`(?is)<(script|style|noscript)[^>]*>.*?</(script|style|noscript)>`)
	tagRe    = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRe     = regexp.MustCompile(`[ \t]+`)
	nlRe     = regexp.MustCompile(`\n\s*\n+`)
)

func HTMLToText(s string) string {
	s = scriptRe.ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/h\d)[^>]*>`).ReplaceAllString(s, "\n")
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(nlRe.ReplaceAllString(s, "\n\n"))
}

func searx(ctx context.Context, base, q string) (string, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/search")
	if err != nil {
		return "", err
	}
	qs := u.Query()
	qs.Set("q", q)
	qs.Set("format", "json")
	u.RawQuery = qs.Encode()
	// The search backend is the owner's own instance, so it may be on
	// localhost; use a plain client for it, never for user-supplied URLs.
	b, err := fetchTrusted(ctx, u.String())
	if err != nil {
		return "", err
	}
	var v struct {
		Results []struct{ Title, URL, Content string } `json:"results"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return "", err
	}
	var out strings.Builder
	for i, r := range v.Results {
		if i >= 5 {
			break
		}
		fmt.Fprintf(&out, "%d. %s\n   %s\n   %s\n", i+1, r.Title, r.URL, r.Content)
	}
	if out.Len() == 0 {
		return "no results", nil
	}
	return out.String(), nil
}

// ParseDue reads a due date a model wrote: RFC3339 or a plain YYYY-MM-DD
// (noon local, so a day never slips across a time zone). Empty is no date.
func ParseDue(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, errors.New("due must be RFC3339 or YYYY-MM-DD")
	}
	return t.Add(12 * time.Hour), nil
}
