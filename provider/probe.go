package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/config"
)

// Diagnosis is what a live check found, written for the person who has to fix
// it: Problem says what is wrong, Fix says what to do about it.
type Diagnosis struct {
	OK      bool
	Latency time.Duration
	Reply   string
	Problem string
	Fix     string
	Models  []string // close matches from the provider's own model list
}

// Probe makes one tiny real request and explains the failure in plain words.
// It never returns or logs the key.
func Probe(ctx context.Context, name string, c config.Provider) Diagnosis {
	if c.APIKeyEnv != "" && c.Key() == "" && c.APIKey == "" {
		return Diagnosis{Problem: "no key: the environment variable " + c.APIKeyEnv + " is empty",
			Fix: "put " + c.APIKeyEnv + "=your-key in .makizu/.env (or export it) and start her again"}
	}
	if c.BaseURL == "" || c.Model == "" {
		return Diagnosis{Problem: "base_url or model is empty", Fix: "pick a preset in the panel or fill both in config.json"}
	}
	h := NewHTTP(name, c)
	// reasoning models burn tokens thinking before the first visible word, so a
	// stingy budget would report a working setup as broken
	r, err := h.Complete(ctx, Request{System: "Answer with the single word: ok", MaxTokens: 400, Messages: []Message{{Role: User, Content: "ping"}}})
	if err == nil {
		return Diagnosis{OK: true, Latency: r.Latency, Reply: strings.TrimSpace(r.Text)}
	}
	d := explain(err, c)
	if pe, ok := err.(*Error); ok && (pe.Kind == KindBadRequest) && pe.Status != 0 {
		d.Models = suggestModels(ctx, h, c.Model)
	}
	return d
}

func explain(err error, c config.Provider) Diagnosis {
	pe, ok := err.(*Error)
	if !ok {
		return Diagnosis{Problem: err.Error(), Fix: "check the base_url"}
	}
	low := strings.ToLower(pe.Msg)
	host := c.BaseURL
	if u, e := url.Parse(c.BaseURL); e == nil {
		host = u.Host
	}
	switch {
	case strings.Contains(low, "connection refused") || strings.Contains(low, "no such host") || strings.Contains(low, "dial tcp"):
		return Diagnosis{Problem: "nothing answered at " + host, Fix: "check the base_url; for a local server (Ollama, LM Studio) make sure it is running"}
	case pe.Kind == KindTimeout:
		return Diagnosis{Problem: "no answer from " + host + " in time", Fix: "check your connection or raise timeout_seconds (local models can be slow to load)"}
	case pe.Status == 403 && (strings.Contains(low, "1010") || strings.Contains(low, "cloudflare") || strings.Contains(low, "error code")):
		return Diagnosis{Problem: host + " blocked the request before it reached the API (bot protection)", Fix: "a VPN or datacenter IP is the usual cause; set a custom User-Agent in the provider headers if it persists"}
	case pe.Kind == KindAuth:
		return Diagnosis{Problem: host + " rejected the key (" + itoa(pe.Status) + ")", Fix: "the key is wrong, expired, or has no access to model " + c.Model}
	case pe.Kind == KindRateLimit:
		return Diagnosis{Problem: "rate limited or out of quota", Fix: "check the plan and balance on the provider's dashboard"}
	case strings.Contains(low, "session"):
		return Diagnosis{Problem: host + " wants a session header", Fix: "add it under headers for this provider"}
	case pe.Kind == KindEmpty:
		return Diagnosis{Problem: "the model answered with nothing visible (it spent the budget thinking)", Fix: "raise reasoning_headroom, or lower reasoning_effort"}
	case pe.Status == 404 || strings.Contains(low, "model"):
		return Diagnosis{Problem: "model " + c.Model + " or the path was not found at " + host, Fix: "check the model name and that base_url ends where the provider says (usually /v1)"}
	case pe.Kind == KindBadRequest:
		return Diagnosis{Problem: "the provider refused the request: " + pe.Msg, Fix: "usually the wrong protocol (chat vs responses) or an unsupported parameter"}
	}
	return Diagnosis{Problem: pe.Msg, Fix: "see the log for details"}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// suggestModels asks the provider what it actually serves and returns the
// closest ids to the one that failed.
func suggestModels(ctx context.Context, h *HTTP, want string) []string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.Cfg.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil
	}
	h.decorate(req)
	resp, err := h.HC.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var v struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	w := strings.ToLower(want)
	type scored struct {
		id string
		n  int
	}
	var all []scored
	for _, m := range v.Data {
		all = append(all, scored{m.ID, sharedPrefix(strings.ToLower(m.ID), w)})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].n > all[j].n })
	var out []string
	for i := 0; i < len(all) && i < 5; i++ {
		out = append(out, all[i].id)
	}
	return out
}

func sharedPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}
