package distill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/voice"
)

// LLM is the one model call distilling needs.
type LLM interface {
	Complete(ctx context.Context, r provider.Request) (provider.Response, error)
}

// Options says what to learn from and where to put it.
type Options struct {
	Msgs    []Msg
	As      string // whose messages; empty means the most frequent author
	ID      string // persona id; empty means a slug of the speaker's name
	Dir     string // the personas directory
	Rounds  int    // refinement rounds against the measured targets (default 2)
	Force   bool   // overwrite an existing persona
	Confirm func(summary string) bool
	Say     func(format string, a ...any)
	MaxTok  int
}

// Result is what came out.
type Result struct {
	ID       string
	Speaker  string
	Messages int
	Stats    voice.Stats // how the person really writes
	Targets  voice.Targets
	Final    voice.Stats // how the character writes, last round
	Fails    []string    // what is still outside the targets ("" when it passes)
	Files    []string
}

const minMessages = 30

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "distilled"
	}
	if len(s) > 32 {
		s = strings.Trim(s[:32], "-")
	}
	return s
}

// Sample picks n messages spread across the lengths this person writes, so
// the sample shows the one-word answers as well as the paragraphs.
func Sample(msgs []Msg, n int) []string {
	seen := map[string]bool{}
	var u []string
	for _, m := range msgs {
		k := strings.ToLower(m.Text)
		if !seen[k] && len([]rune(m.Text)) <= 400 {
			seen[k] = true
			u = append(u, m.Text)
		}
	}
	sort.SliceStable(u, func(i, j int) bool { return len([]rune(u[i])) < len([]rune(u[j])) })
	if len(u) <= n {
		return u
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, u[i*(len(u)-1)/(n-1)])
	}
	return out
}

// Prompts are things other people said that this person answered: what to say
// to the character to see how it answers.
func Prompts(msgs []Msg, n int) []string {
	seen := map[string]bool{}
	var u []string
	for _, m := range msgs {
		l := len([]rune(m.Prev))
		if l >= 3 && l <= 200 && !seen[strings.ToLower(m.Prev)] {
			seen[strings.ToLower(m.Prev)] = true
			u = append(u, m.Prev)
		}
	}
	if len(u) <= n {
		return u
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, u[i*(len(u)-1)/(n-1)])
	}
	return out
}

const draftSystem = `You write character files for a chat companion. A character file is Markdown with a short front matter and a body that tells the model who to be. You are given real messages written by one person and measurements of how they write. Write a character that talks like that person, as a character: temperament, what they care about, how they treat people, how they type.

Format, exactly (output only the file, nothing before or after):
---
name: <a short first name or handle for the character>
language: auto
---
# Who you are
(2-3 short paragraphs: temperament and worldview, inferred from the messages)
## How you type
(casing, punctuation, length, swearing, emoji, repeated habits: concrete, measured, matching the numbers you were given)
## What's in your orbit
(topics and tastes that actually appear in the messages)
## How you treat people
## Examples
(3 to 5 tiny exchanges in this voice, invented, not copied)
## Never
(what would break the character)

Rules: never copy a sentence from the samples, never include a real name, place, handle, employer or any private detail that appears in them, and invent nothing about their life that the messages do not support. Specific beats adjectives. Keep it under 450 words.`

const reviseSystem = `You revise a character file for a chat companion. When the character was tested, its replies were outside the measured targets of the person it is based on. Rewrite the file so the character's replies fall inside the targets: change how the file describes length, casing, endings and habits, and add or fix examples that show it. Keep the identity. Output only the whole new file.`

func profile(s voice.Stats) string {
	return fmt.Sprintf("median %d words (p90 %d); %.0f%% of messages are 12 words or fewer; %.0f%% entirely lowercase; %.0f%% end on a question; %.0f%% contain a SHOUTED word; most repeated opening %q in %.0f%%",
		s.MedianWords, s.P90Words, s.ShortPct, s.LowerPct, s.QuestionPct, s.CapsPct, s.TopOpener, s.TopOpenerPct)
}

// Run learns a character from a person's messages. It returns an error before
// any model call if there is too little to learn from or the person declines.
func Run(ctx context.Context, llm LLM, o Options) (Result, error) {
	say := o.Say
	if say == nil {
		say = func(string, ...any) {}
	}
	who, err := Speaker(o.Msgs, o.As)
	if err != nil {
		return Result{}, err
	}
	own := Own(o.Msgs, who)
	res := Result{Speaker: who, Messages: len(own)}
	if len(own) < minMessages {
		return res, fmt.Errorf("%s wrote %d usable messages; a voice needs at least %d to be measured fairly", who, len(own), minMessages)
	}
	texts := make([]string, len(own))
	for i, m := range own {
		texts[i] = m.Text
	}
	res.Stats = voice.Measure(texts)
	res.Targets = voice.FromStats(res.Stats)
	res.ID = o.ID
	if res.ID == "" {
		res.ID = slug(who)
	}
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(res.ID) {
		return res, fmt.Errorf("persona id %q must use a-z, 0-9 and -", res.ID)
	}
	dir := filepath.Join(o.Dir, res.ID)
	if _, err := os.Stat(dir); err == nil && !o.Force {
		return res, fmt.Errorf("persona %q already exists; pick another --id or pass --force", res.ID)
	}
	samples := Sample(own, 40)
	inputs := Prompts(own, 20)
	if len(inputs) < 6 {
		inputs = voice.DefaultInputs
	}

	summary := fmt.Sprintf("%s wrote %d messages. I measured them (%s). To write the character I will send %d of those messages, with links, mentions, emails and phone numbers removed, to the model provider you configured, plus a few model calls to test the character (up to %d short test messages, none of them from the export). The export itself never leaves this machine and nothing is stored except the character file.",
		who, len(own), profile(res.Stats), len(samples), o.rounds()*min(len(inputs), 12))
	if o.Confirm != nil && !o.Confirm(summary) {
		return res, errors.New("cancelled: nothing was sent")
	}

	say("writing a first draft from %d samples", len(samples))
	text, err := o.draft(ctx, llm, res.Stats, samples)
	if err != nil {
		return res, err
	}
	best, bestStats, bestFails := text, voice.Stats{}, []string(nil)
	score := 1 << 30
	evalSet := inputs[:min(len(inputs), 12)]
	for round := 0; ; round++ {
		replies, err := o.reply(ctx, llm, text, res.ID, evalSet)
		if err != nil {
			return res, err
		}
		st := voice.Measure(replies)
		fails := st.Check(res.Targets)
		say("round %d: median %d words, %d outside target", round+1, st.MedianWords, len(fails))
		if len(fails) < score {
			best, bestStats, bestFails, score = text, st, fails, len(fails)
		}
		if len(fails) == 0 || round+1 >= o.rounds() {
			break
		}
		next, err := o.revise(ctx, llm, text, fails, replies, res.Targets)
		if err != nil {
			say("could not revise (%v); keeping the best so far", err)
			break
		}
		text = next
	}
	res.Final, res.Fails = bestStats, bestFails

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	tj, _ := json.MarshalIndent(res.Targets, "", "  ")
	for name, body := range map[string]string{
		"persona.md": strings.TrimSpace(best) + "\n",
		"voice.json": string(tj) + "\n",
		"eval.txt":   "# things to say to this character, taken from what people said to the real person\n" + strings.Join(inputs, "\n") + "\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return res, err
		}
		res.Files = append(res.Files, filepath.Join(dir, name))
	}
	sort.Strings(res.Files)
	return res, nil
}

func (o Options) rounds() int {
	switch {
	case o.Rounds <= 0:
		return 2
	case o.Rounds > 4:
		return 4
	}
	return o.Rounds
}

func (o Options) maxTok() int {
	if o.MaxTok > 0 {
		return o.MaxTok
	}
	return 900
}

func (o Options) draft(ctx context.Context, llm LLM, st voice.Stats, samples []string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "How this person writes: %s.\n\nTheir messages (one per line, shortest to longest):\n", profile(st))
	for _, s := range samples {
		b.WriteString("- " + s + "\n")
	}
	var last error
	for try := 0; try < 2; try++ {
		cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
		r, err := llm.Complete(cctx, provider.Request{System: draftSystem, MaxTokens: 1400, Messages: []provider.Message{{Role: provider.User, Content: b.String()}}})
		cancel()
		if err != nil {
			last = err
			continue
		}
		text := stripFence(r.Text)
		if _, err := persona.Parse("x", text); err != nil {
			last = fmt.Errorf("the model did not write a valid character file: %w", err)
			continue
		}
		return text, nil
	}
	return "", last
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	}
	return strings.TrimSpace(s)
}

func (o Options) reply(ctx context.Context, llm LLM, text, id string, inputs []string) ([]string, error) {
	pa, err := persona.Parse(id, text)
	if err != nil {
		return nil, err
	}
	sys := persona.Compose(pa, persona.Context{Now: time.Now().Format(time.RFC1123), Speaker: "Test", Place: "a test channel",
		LanguageHint: "Reply in English until the person writes in another language; then switch to theirs."})
	var out []string
	for _, in := range inputs {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		r, err := llm.Complete(cctx, provider.Request{System: sys, MaxTokens: o.maxTok(), Messages: []provider.Message{{Role: provider.User, Content: "Test: " + in}}})
		cancel()
		if err != nil {
			continue
		}
		if c, v := guard.Clean(r.Text); v == guard.OK {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the draft could not be tested: no usable replies")
	}
	return out, nil
}

func (o Options) revise(ctx context.Context, llm LLM, text string, fails, replies []string, t voice.Targets) (string, error) {
	var b strings.Builder
	b.WriteString("The character file:\n\n" + text + "\n\nWhen tested it was outside these targets:\n")
	for _, f := range fails {
		b.WriteString("- " + f + "\n")
	}
	fmt.Fprintf(&b, "\nTargets to land inside: median length %d to %d words, at most %.0f%% of replies ending on a question, at most %.0f%% opening with an action beat.\n\nSome of its replies:\n", t.MedianWords[0], t.MedianWords[1], t.MaxQuestionPct, t.MaxActionPct)
	for i, r := range replies {
		if i == 6 {
			break
		}
		b.WriteString("- " + strings.ReplaceAll(r, "\n", " ") + "\n")
	}
	cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	r, err := llm.Complete(cctx, provider.Request{System: reviseSystem, MaxTokens: 1400, Messages: []provider.Message{{Role: provider.User, Content: b.String()}}})
	if err != nil {
		return "", err
	}
	next := stripFence(r.Text)
	if _, err := persona.Parse("x", next); err != nil {
		return "", err
	}
	return next, nil
}
