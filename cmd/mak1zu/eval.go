package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
)

// cmdEval replays a file of inputs (one per line) against the active persona
// and the real provider, then prints the same voice metrics VOICE.md measured
// on the original companion, so a persona edit can be judged with numbers instead of vibes.
func cmdEval(st *config.Store, path string) error {
	cfg := st.Get()
	pa, err := persona.Library{Dir: st.Abs(cfg.Persona.Dir)}.Load(cfg.Persona.Active)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var inputs []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
			inputs = append(inputs, l)
		}
	}
	router := provider.NewRouter(st.Get)
	sys := persona.Compose(pa, persona.Context{Now: time.Now().Format(time.RFC1123), Speaker: "Test", Place: "a test channel", LanguageHint: "Reply in the language the person is writing in."})
	var words []int
	var n, lower, action, robotic, qEnd, failed int
	for _, in := range inputs {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		r, err := router.Complete(ctx, provider.Request{System: sys, MaxTokens: cfg.Behavior.Turn.MaxTokens,
			Messages: []provider.Message{{Role: provider.User, Content: "Test: " + in}}})
		cancel()
		if err != nil {
			failed++
			fmt.Printf("✗ %q -> %v\n", in, err)
			continue
		}
		out, v := guard.Clean(r.Text)
		if v != guard.OK {
			failed++
			fmt.Printf("✗ %q -> guard: %s\n", in, v)
			continue
		}
		n++
		words = append(words, len(strings.Fields(out)))
		if out == strings.ToLower(out) {
			lower++
		}
		if guard.OpensWithAction(out) {
			action++
		}
		if len(guard.RoboticHits(out)) > 0 {
			robotic++
		}
		if strings.HasSuffix(strings.TrimSpace(out), "?") {
			qEnd++
		}
		fmt.Printf("> %s\n  %s\n", in, strings.ReplaceAll(out, "\n", "\n  "))
	}
	if n == 0 {
		return fmt.Errorf("no usable replies (%d failed)", failed)
	}
	sort.Ints(words)
	pct := func(x int) float64 { return 100 * float64(x) / float64(n) }
	fmt.Printf("\n%s over %d replies (%d failed)\n", pa.Name, n, failed)
	fmt.Printf("  words: median %d, p90 %d      (reference companion: median 30, p95 87)\n", words[n/2], words[min(n-1, n*9/10)])
	fmt.Printf("  all-lowercase %.0f%%  action-opener %.0f%%  ends-with-? %.0f%%  robotic tells %.0f%%\n", pct(lower), pct(action), pct(qEnd), pct(robotic))
	fmt.Println("  targets: action-opener < 20%, ends-with-? < 10%, robotic tells 0%")
	return nil
}
