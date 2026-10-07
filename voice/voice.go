// Package voice measures how a character sounds, in numbers, and checks it
// against targets. It is what turns "make her sound like X" from a feeling
// into something a test can fail: reply length, casing, question endings,
// action openers, repeated openers and phrases, and the tells of customer
// support. The same measurements describe a person's real messages (to build
// a persona from them) and a persona's replies (to check it kept the promise).
package voice

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/snowarch/mak1zu/guard"
)

// Stats are the measurements of a set of messages. Percentages are 0-100.
type Stats struct {
	N           int     `json:"n"`
	MedianWords int     `json:"median_words"`
	P90Words    int     `json:"p90_words"`
	ShortPct    float64 `json:"short_pct"`    // 12 words or fewer
	LongPct     float64 `json:"long_pct"`     // more than 80 words
	LowerPct    float64 `json:"lower_pct"`    // entirely lowercase
	ActionPct   float64 `json:"action_pct"`   // opens with *an action beat*
	QuestionPct float64 `json:"question_pct"` // ends on a question mark
	RoboticPct  float64 `json:"robotic_pct"`  // customer-support tells
	CapsPct     float64 `json:"caps_pct"`     // contains a SHOUTED word

	TopOpener    string  `json:"top_opener"`     // the most repeated first two words
	TopOpenerPct float64 `json:"top_opener_pct"` // share of messages that open with it
	TopPhrase    string  `json:"top_phrase"`     // the most repeated three-word phrase
	TopPhrasePct float64 `json:"top_phrase_pct"` // share of messages containing it
}

var (
	wordRe = regexp.MustCompile(`[\p{L}\p{N}']+`)
	capsRe = regexp.MustCompile(`\b[A-Z]{3,}\b`)
)

func words(s string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		out = append(out, w)
	}
	return out
}

// Measure describes replies. Empty messages are ignored.
func Measure(replies []string) Stats {
	var s Stats
	var counts []int
	opener := map[string]int{}
	phrase := map[string]int{}
	var short, long, lower, action, q, robotic, caps int
	for _, r := range replies {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		s.N++
		w := words(r)
		counts = append(counts, len(strings.Fields(r)))
		if n := len(strings.Fields(r)); n <= 12 {
			short++
		} else if n > 80 {
			long++
		}
		if r == strings.ToLower(r) && strings.IndexFunc(r, unicode.IsLetter) >= 0 {
			lower++
		}
		if guard.OpensWithAction(r) {
			action++
		}
		if strings.HasSuffix(r, "?") {
			q++
		}
		if len(guard.RoboticHits(r)) > 0 {
			robotic++
		}
		if capsRe.MatchString(r) {
			caps++
		}
		if len(w) >= 2 {
			opener[w[0]+" "+w[1]]++
		} else if len(w) == 1 {
			opener[w[0]]++
		}
		seen := map[string]bool{}
		for i := 0; i+2 < len(w); i++ {
			p := w[i] + " " + w[i+1] + " " + w[i+2]
			if !seen[p] {
				seen[p] = true
				phrase[p]++
			}
		}
	}
	if s.N == 0 {
		return s
	}
	sort.Ints(counts)
	s.MedianWords, s.P90Words = counts[s.N/2], counts[min(s.N-1, s.N*9/10)]
	pct := func(x int) float64 { return 100 * float64(x) / float64(s.N) }
	s.ShortPct, s.LongPct, s.LowerPct, s.ActionPct, s.QuestionPct, s.RoboticPct, s.CapsPct =
		pct(short), pct(long), pct(lower), pct(action), pct(q), pct(robotic), pct(caps)
	top := func(m map[string]int) (string, float64) {
		best, n := "", 0
		for k, v := range m {
			if v > n || (v == n && k < best) {
				best, n = k, v
			}
		}
		return best, pct(n)
	}
	s.TopOpener, s.TopOpenerPct = top(opener)
	s.TopPhrase, s.TopPhrasePct = top(phrase)
	return s
}

// Targets are the bounds a character promises to stay inside.
type Targets struct {
	MedianWords    [2]int  `json:"median_words"` // lowest and highest acceptable median length
	MaxP90Words    int     `json:"max_p90_words"`
	MaxActionPct   float64 `json:"max_action_pct"`
	MaxQuestionPct float64 `json:"max_question_pct"`
	MaxRoboticPct  float64 `json:"max_robotic_pct"`
	MaxOpenerPct   float64 `json:"max_top_opener_pct"` // one way of starting must not dominate
	MaxPhrasePct   float64 `json:"max_top_phrase_pct"` // nor one phrase turn into a tic
	MinLowerPct    float64 `json:"min_lower_pct,omitempty"`
	MaxLowerPct    float64 `json:"max_lower_pct,omitempty"`
}

// DefaultTargets are the floor for any character: the numbers VOICE.md
// measured on a real companion, with room.
func DefaultTargets() Targets {
	return Targets{
		MedianWords: [2]int{4, 45}, MaxP90Words: 110,
		MaxActionPct: 20, MaxQuestionPct: 15, MaxRoboticPct: 0,
		MaxOpenerPct: 30, MaxPhrasePct: 20,
		MaxLowerPct: 100,
	}
}

// LoadTargets reads a voice.json and lays it over the defaults, so a file only
// has to say what is different about this character.
func LoadTargets(path string) (Targets, error) {
	t := DefaultTargets()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return t, nil
		}
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return t, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// Check returns what is out of bounds, in words a person can act on. A
// sample too small to judge repetition (under 12 replies) is not held to it.
func (s Stats) Check(t Targets) []string {
	var out []string
	add := func(f string, a ...any) { out = append(out, fmt.Sprintf(f, a...)) }
	if s.N == 0 {
		return []string{"no replies to judge"}
	}
	if s.MedianWords < t.MedianWords[0] || s.MedianWords > t.MedianWords[1] {
		add("median reply is %d words; this character should sit between %d and %d", s.MedianWords, t.MedianWords[0], t.MedianWords[1])
	}
	if t.MaxP90Words > 0 && s.P90Words > t.MaxP90Words {
		add("1 in 10 replies is longer than %d words (limit %d)", s.P90Words, t.MaxP90Words)
	}
	if s.ActionPct > t.MaxActionPct {
		add("%.0f%% of replies open with an action beat (limit %.0f%%)", s.ActionPct, t.MaxActionPct)
	}
	if s.QuestionPct > t.MaxQuestionPct {
		add("%.0f%% of replies end on a question (limit %.0f%%): she is begging for engagement", s.QuestionPct, t.MaxQuestionPct)
	}
	if s.RoboticPct > t.MaxRoboticPct {
		add("%.0f%% of replies sound like customer support", s.RoboticPct)
	}
	if t.MinLowerPct > 0 && s.LowerPct < t.MinLowerPct {
		add("only %.0f%% of replies are all lowercase (should be at least %.0f%%)", s.LowerPct, t.MinLowerPct)
	}
	if t.MaxLowerPct > 0 && t.MaxLowerPct < 100 && s.LowerPct > t.MaxLowerPct {
		add("%.0f%% of replies are all lowercase (limit %.0f%%)", s.LowerPct, t.MaxLowerPct)
	}
	if s.N >= 12 {
		if s.TopOpenerPct > t.MaxOpenerPct {
			add("%.0f%% of replies start with %q (limit %.0f%%)", s.TopOpenerPct, s.TopOpener, t.MaxOpenerPct)
		}
		if s.TopPhrasePct > t.MaxPhrasePct {
			add("%.0f%% of replies contain %q (limit %.0f%%): a tic", s.TopPhrasePct, s.TopPhrase, t.MaxPhrasePct)
		}
	}
	return out
}

// FromStats derives targets from how a real person writes, with room on both
// sides, so a character distilled from them is held to resembling them.
func FromStats(s Stats) Targets {
	t := DefaultTargets()
	if s.N == 0 {
		return t
	}
	t.MedianWords = [2]int{max(2, s.MedianWords*6/10), max(8, s.MedianWords*16/10)}
	t.MaxP90Words = max(30, s.P90Words*14/10)
	t.MaxActionPct = min(20, s.ActionPct+10)
	t.MaxQuestionPct = min(30, s.QuestionPct+10)
	// a person's own habits are part of the voice: only hold her to repeating
	// no more than they do
	t.MaxOpenerPct = min(60, max(t.MaxOpenerPct, s.TopOpenerPct+10))
	t.MaxPhrasePct = min(50, max(t.MaxPhrasePct, s.TopPhrasePct+10))
	switch {
	case s.LowerPct >= 70:
		t.MinLowerPct = math.Floor(s.LowerPct - 25)
	case s.LowerPct <= 15:
		t.MaxLowerPct = math.Ceil(s.LowerPct + 25)
	}
	t.MaxOpenerPct, t.MaxPhrasePct = math.Ceil(t.MaxOpenerPct), math.Ceil(t.MaxPhrasePct)
	return t
}

// DefaultInputs are what `mak1zu eval` says to a character when nothing else
// is given: a spread of the situations that expose a voice (a greeting, a
// one-word message, an insult, a sad moment, a factual question, a request
// for a tool, a language switch).
var DefaultInputs = []string{
	"hey", "lol", "ok", "what's your favorite anime", "are you a bot?", "i had a rough day",
	"you're annoying", "tabs or spaces", "i think i broke my config", "who are you and why are you here",
	"can you recommend something to watch tonight", "hola, ¿qué tal?", "thanks, that actually helped",
	"i've been awake for 30 hours and finals are on monday", "do you ever get bored", "say something nice about me",
}
