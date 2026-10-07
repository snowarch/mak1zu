package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/voice"
)

// readInputs reads one message per line; blank lines and # comments are skipped.
func readInputs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out, sc.Err()
}

// cmdEval says a list of things to the active character through the real
// provider and judges how it sounds, with numbers. Inputs come from the file
// given, else from the character's own eval.txt, else a built-in spread.
// Targets come from the character's voice.json, laid over the defaults. With
// --gate, a reply set outside its targets is a failure (exit 1), so a persona
// edit can be refused the way a failing test refuses a code edit.
func cmdEval(st *config.Store, args []string) error {
	gate, path := false, ""
	for _, a := range args {
		if a == "--gate" {
			gate = true
		} else {
			path = a
		}
	}
	cfg := st.Get()
	lib := persona.Library{Dir: st.Abs(cfg.Persona.Dir)}
	pa, err := lib.Load(cfg.Persona.Active)
	if err != nil {
		return err
	}
	dir := filepath.Join(lib.Dir, pa.ID)
	inputs := voice.DefaultInputs
	switch {
	case path != "":
		if inputs, err = readInputs(path); err != nil {
			return err
		}
	default:
		if in, err := readInputs(filepath.Join(dir, "eval.txt")); err == nil && len(in) > 0 {
			inputs = in
		}
	}
	targets, err := voice.LoadTargets(filepath.Join(dir, "voice.json"))
	if err != nil {
		return err
	}
	router := provider.NewRouter(st.Get)
	replies, failed, err := askAll(context.Background(), router, pa, cfg.Behavior.Turn.MaxTokens, inputs, true)
	if err != nil {
		return err
	}
	return report(pa.Name, replies, failed, targets, gate)
}

// askAll puts each input to a character and returns the replies the guard
// would let through.
func askAll(ctx context.Context, c interface {
	Complete(context.Context, provider.Request) (provider.Response, error)
}, pa persona.Persona, maxTok int, inputs []string, show bool) (replies []string, failed int, err error) {
	sys := persona.Compose(pa, persona.Context{Now: time.Now().Format(time.RFC1123), Speaker: "Test", Place: "a test channel",
		LanguageHint: "Reply in English until the person writes in another language; then switch to theirs."})
	for _, in := range inputs {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		r, e := c.Complete(cctx, provider.Request{System: sys, MaxTokens: maxTok,
			Messages: []provider.Message{{Role: provider.User, Content: "Test: " + in}}})
		cancel()
		if e != nil {
			failed++
			if show {
				fmt.Printf("✗ %q -> %v\n", in, e)
			}
			continue
		}
		out, v := guard.Clean(r.Text)
		if v != guard.OK {
			failed++
			if show {
				fmt.Printf("✗ %q -> guard: %s\n", in, v)
			}
			continue
		}
		replies = append(replies, out)
		if show {
			fmt.Printf("> %s\n  %s\n", in, strings.ReplaceAll(out, "\n", "\n  "))
		}
	}
	if len(replies) == 0 {
		return nil, failed, fmt.Errorf("no usable replies (%d failed)", failed)
	}
	return replies, failed, nil
}

func report(name string, replies []string, failed int, t voice.Targets, gate bool) error {
	s := voice.Measure(replies)
	fmt.Printf("\n%s over %d replies (%d failed)\n", name, s.N, failed)
	fmt.Printf("  words: median %d, p90 %d (targets %d-%d, p90 <= %d)\n", s.MedianWords, s.P90Words, t.MedianWords[0], t.MedianWords[1], t.MaxP90Words)
	fmt.Printf("  all-lowercase %.0f%%  action-opener %.0f%%  ends-with-? %.0f%%  robotic tells %.0f%%  caps bursts %.0f%%\n", s.LowerPct, s.ActionPct, s.QuestionPct, s.RoboticPct, s.CapsPct)
	fmt.Printf("  most repeated opener: %q in %.0f%%   most repeated phrase: %q in %.0f%%\n", s.TopOpener, s.TopOpenerPct, s.TopPhrase, s.TopPhrasePct)
	fails := s.Check(t)
	if len(fails) == 0 {
		fmt.Println("  inside every target")
		return nil
	}
	for _, f := range fails {
		fmt.Println("  outside:", f)
	}
	if gate {
		return errors.New("the voice is outside its targets")
	}
	return nil
}
