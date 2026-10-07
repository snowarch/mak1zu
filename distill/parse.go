// Package distill turns somebody's real messages into a character: it reads a
// chat export, measures how that person writes, has the model draft a
// persona file from a sample, and holds the result to the measured numbers
// until it sounds like them. Everything is local until the one model call,
// and that call is only made after the person says yes to what it will send.
package distill

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Msg is one message of an export.
type Msg struct {
	Author string
	Text   string
	Prev   string // what the previous message (by someone else) said, if known
}

// Load reads a chat export. It understands DiscordChatExporter JSON, Discord's
// own data package (messages.json), WhatsApp text exports, "Name: message"
// lines, and plain one-message-per-line text.
func Load(path string) ([]Msg, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return nil, errors.New("the export is empty")
	}
	switch t[0] {
	case '{', '[':
		return fromJSON(t)
	}
	return fromText(string(b)), nil
}

func fromJSON(b []byte) ([]Msg, error) {
	// DiscordChatExporter: {"messages":[{"author":{"name":..},"content":..}]}
	var dce struct {
		Messages []struct {
			Content string `json:"content"`
			Author  struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Nickname string `json:"nickname"`
				IsBot    bool   `json:"isBot"`
			} `json:"author"`
		} `json:"messages"`
	}
	if json.Unmarshal(b, &dce) == nil && len(dce.Messages) > 0 {
		var out []Msg
		prev := ""
		for _, m := range dce.Messages {
			if m.Author.IsBot {
				continue
			}
			name := firstNonEmpty(m.Author.Nickname, m.Author.Name, m.Author.ID)
			out = append(out, Msg{Author: name, Text: m.Content, Prev: prev})
			prev = m.Content
		}
		return out, nil
	}
	// Discord data package: [{"ID":..,"Timestamp":..,"Contents":..}], only the person's own messages
	var pkg []struct {
		Contents string `json:"Contents"`
	}
	if json.Unmarshal(b, &pkg) == nil && len(pkg) > 0 && pkg[0].Contents != "" {
		var out []Msg
		for _, m := range pkg {
			out = append(out, Msg{Author: "me", Text: m.Contents})
		}
		return out, nil
	}
	return nil, errors.New("this JSON is not a chat export I know (DiscordChatExporter or a Discord data package)")
}

var (
	waRe   = regexp.MustCompile(`^\[?\d{1,4}[/.\-]\d{1,2}[/.\-]\d{1,4},? \d{1,2}:\d{2}(?::\d{2})?(?:\s?[APap][Mm])?\]?\s*(?:-\s*)?([^:]{1,40}): (.*)$`)
	nameRe = regexp.MustCompile(`^([\p{L}\p{N}_ .'-]{1,32}): (.+)$`)
)

func fromText(s string) []Msg {
	var out []Msg
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	prev, namedLines, lines := "", 0, 0
	var raw []string
	for sc.Scan() {
		raw = append(raw, sc.Text())
	}
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			lines++
			if waRe.MatchString(l) || nameRe.MatchString(l) {
				namedLines++
			}
		}
	}
	named := lines > 0 && float64(namedLines)/float64(lines) > 0.6
	for _, l := range raw {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if named {
			if m := waRe.FindStringSubmatch(l); m != nil {
				out = append(out, Msg{Author: strings.TrimSpace(m[1]), Text: m[2], Prev: prev})
				prev = m[2]
				continue
			}
			if m := nameRe.FindStringSubmatch(l); m != nil {
				out = append(out, Msg{Author: strings.TrimSpace(m[1]), Text: m[2], Prev: prev})
				prev = m[2]
				continue
			}
			// a continuation of the previous message
			if n := len(out); n > 0 {
				out[n-1].Text += "\n" + l
				continue
			}
		}
		out = append(out, Msg{Author: "me", Text: l})
	}
	return out
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Speaker picks whose messages to learn from: the name given with --as
// (matched on the whole name, case-insensitively), or the most frequent author.
func Speaker(msgs []Msg, as string) (string, error) {
	count := map[string]int{}
	for _, m := range msgs {
		count[m.Author]++
	}
	if as != "" {
		for a := range count {
			if strings.EqualFold(a, as) {
				return a, nil
			}
		}
		return "", fmt.Errorf("nobody called %q in this export; the authors are: %s", as, authors(count))
	}
	if len(count) == 1 {
		for a := range count {
			return a, nil
		}
	}
	best, n := "", 0
	for a, c := range count {
		if c > n || (c == n && a < best) {
			best, n = a, c
		}
	}
	if best == "" {
		return "", errors.New("no messages")
	}
	return best, nil
}

func authors(count map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var l []kv
	for k, v := range count {
		l = append(l, kv{k, v})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].v > l[j].v || (l[i].v == l[j].v && l[i].k < l[j].k) })
	var parts []string
	for i, x := range l {
		if i == 8 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", x.k, x.v))
	}
	return strings.Join(parts, ", ")
}

var (
	urlRe     = regexp.MustCompile(`https?://\S+|www\.\S+`)
	mentionRe = regexp.MustCompile(`<@[!&]?\d+>|<#\d+>|@\w[\w.]{1,31}`)
	emailRe   = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	phoneRe   = regexp.MustCompile(`\+?\d[\d\s().-]{7,}\d`)
	codeRe    = regexp.MustCompile("(?s)```.*?```")
	emojiRe   = regexp.MustCompile(`<a?:(\w+):\d+>`)
)

// Clean removes what is not voice and what is not theirs to share: links,
// mentions, addresses, phone numbers, code blocks. Custom emoji keep their name.
func Clean(s string) string {
	s = codeRe.ReplaceAllString(s, " ")
	s = emojiRe.ReplaceAllString(s, ":$1:")
	s = emailRe.ReplaceAllString(s, " ")
	s = urlRe.ReplaceAllString(s, " ")
	s = mentionRe.ReplaceAllString(s, " ")
	s = phoneRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// Own returns the cleaned messages of one speaker, with the cleaned thing they
// were answering where there is one.
func Own(msgs []Msg, who string) []Msg {
	var out []Msg
	for _, m := range msgs {
		if m.Author != who {
			continue
		}
		t := Clean(m.Text)
		if len([]rune(t)) < 1 {
			continue
		}
		out = append(out, Msg{Author: who, Text: t, Prev: Clean(m.Prev)})
	}
	return out
}
