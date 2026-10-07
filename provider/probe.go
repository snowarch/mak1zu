package provider

import (
	"context"
	"encoding/json"
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
	Kind    string // error class, "" when OK
	Latency time.Duration
	Reply   string
	Problem string
	Fix     string
	Models  []string // close matches from the provider's own model list
}

// Probe makes one tiny real request and explains the failure in plain words.
// It never returns or logs the key.
func Probe(ctx context.Context, name string, c config.Provider) Diagnosis {
	return ProbeClient(ctx, NewHTTP(name, c), c)
}

// ProbeClient is Probe with the client supplied, so callers (and tests) can
// substitute the transport. Model suggestions only work with the real client.
func ProbeClient(ctx context.Context, cl Client, c config.Provider) Diagnosis {
	if c.APIKeyEnv != "" && c.Key() == "" && c.APIKey == "" {
		return Diagnosis{Problem: "no key: the environment variable " + c.APIKeyEnv + " is empty",
			Fix: "put " + c.APIKeyEnv + "=your-key in .makizu/.env (or export it) and start her again"}
	}
	if c.BaseURL == "" || c.Model == "" {
		return Diagnosis{Problem: "base_url or model is empty", Fix: "pick a preset in the panel or fill both in config.json"}
	}
	// reasoning models burn tokens thinking before the first visible word, so a
	// stingy budget would report a working setup as broken
	r, err := cl.Complete(ctx, Request{System: "Answer with the single word: ok", MaxTokens: 400, Messages: []Message{{Role: User, Content: "ping"}}})
	if err == nil {
		return Diagnosis{OK: true, Latency: r.Latency, Reply: strings.TrimSpace(r.Text)}
	}
	d := explain(err, c)
	d.Kind = KindOf(err).String()
	if h, isHTTP := cl.(*HTTP); isHTTP {
		if pe, ok := err.(*Error); ok && pe.Kind == KindBadRequest && pe.Status != 0 {
			d.Models = suggestModels(ctx, h, c.Model)
		}
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
	case strings.Contains(low, "free tier can only be used from within"):
		return Diagnosis{Problem: "model " + c.Model + " is a free-tier model that only works inside the app that hosts it, not through the API", Fix: "pick a model that is open to API clients (for opencode, the Go list or a paid Zen model) or another preset; docs/PROVIDERS.md says which is which"}
	case pe.Status == 410 || strings.Contains(low, "deprecated") || strings.Contains(low, "retired"):
		return Diagnosis{Problem: "model " + c.Model + " has been retired by " + host + " (" + pe.Msg + ")", Fix: "switch to a current model; the suggestions below come from the provider's own list"}
	case pe.Kind == KindAuth && c.Key() == "":
		return Diagnosis{Problem: host + " wants a key and none is set (" + itoa(pe.Status) + ")", Fix: "set api_key_env on this provider and put the key in .makizu/.env (the panel's Models tab has a write-only key box too)"}
	case pe.Kind == KindAuth:
		return Diagnosis{Problem: host + " rejected the key (" + itoa(pe.Status) + ")", Fix: "the key is wrong, expired, or has no access to model " + c.Model}
	case pe.Kind == KindRateLimit:
		return Diagnosis{Problem: "rate limited or out of quota", Fix: "check the plan and balance on the provider's dashboard"}
	case strings.Contains(low, "session"):
		return Diagnosis{Problem: host + " wants a session header", Fix: "add it under headers for this provider"}
	case pe.Kind == KindEmpty:
		return Diagnosis{Problem: "the model answered with nothing visible (it spent the budget thinking)", Fix: "raise reasoning_headroom, or lower reasoning_effort"}
	case pe.Status == 404 && !strings.Contains(low, "model") && pathless(c.BaseURL):
		return Diagnosis{Problem: "nothing at " + c.BaseURL + "/chat/completions (404)", Fix: "base_url has no path; almost every server wants it to end in /v1, so try " + strings.TrimRight(c.BaseURL, "/") + "/v1"}
	case pe.Status == 404 || strings.Contains(low, "model"):
		return Diagnosis{Problem: "model " + c.Model + " or the path was not found at " + host, Fix: "check the model name and that base_url ends where the provider says (usually /v1)"}
	case pe.Kind == KindBadRequest:
		return Diagnosis{Problem: "the provider refused the request: " + pe.Msg, Fix: "usually the wrong protocol (chat vs responses) or an unsupported parameter"}
	}
	return Diagnosis{Problem: pe.Msg, Fix: "see the log for details"}
}

func pathless(base string) bool {
	u, err := url.Parse(base)
	return err == nil && (u.Path == "" || u.Path == "/")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// suggestModels asks the provider what it actually serves and returns the
// closest ids to the one that failed.
func suggestModels(ctx context.Context, h *HTTP, want string) []string {
	ids, _, err := ListModels(ctx, h.Cfg)
	if err != nil {
		return nil
	}
	w := strings.ToLower(want)
	type scored struct {
		id string
		n  int
	}
	var all []scored
	for _, id := range ids {
		all = append(all, scored{id, sharedPrefix(strings.ToLower(id), w)})
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
