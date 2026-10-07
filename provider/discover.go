package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/config"
)

// ListModels asks an endpoint what it serves (GET <base_url>/models). The
// status is returned so callers can tell "wrong path" (404) from "wants a key"
// (401) from "nothing there" (0). Not every OpenAI-compatible server has this
// route, so a failure is a hint, never a verdict.
func ListModels(ctx context.Context, c config.Provider) (ids []string, status int, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h := NewHTTP("discover", c)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+"/models", nil)
	if err != nil {
		return nil, 0, err
	}
	h.decorate(req)
	resp, err := h.HC.Do(req)
	if err != nil {
		return nil, 0, errors.New(scrub(err.Error(), c.Key()))
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode, fmt.Errorf("%d %s", resp.StatusCode, snippet(data))
	}
	ids = parseModelIDs(data)
	if ids == nil {
		return nil, resp.StatusCode, errors.New("answered, but not with a model list")
	}
	return ids, 200, nil
}

// parseModelIDs reads the shapes servers actually return: OpenAI's
// {"data":[{"id"}]}, Ollama/Cohere-style {"models":[{"name"|"id"}]}, or a bare
// array. nil means "not a model list"; an empty non-nil slice is an empty list.
func parseModelIDs(data []byte) []string {
	type item struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Model string `json:"model"`
	}
	pick := func(items []item) []string {
		out := []string{}
		for _, it := range items {
			switch {
			case it.ID != "":
				out = append(out, it.ID)
			case it.Name != "":
				out = append(out, it.Name)
			case it.Model != "":
				out = append(out, it.Model)
			}
		}
		return out
	}
	var obj struct {
		Data   *[]item `json:"data"`
		Models *[]item `json:"models"`
	}
	if json.Unmarshal(data, &obj) == nil {
		switch {
		case obj.Data != nil:
			return pick(*obj.Data)
		case obj.Models != nil:
			return pick(*obj.Models)
		}
	}
	var arr []item
	if json.Unmarshal(data, &arr) == nil && arr != nil {
		return pick(arr)
	}
	return nil
}

// NormalizeBaseURL turns what people paste into what the client needs: it adds
// a scheme (http for this machine and private networks, https otherwise),
// drops a pasted route (/chat/completions, /responses, /models), a query and
// trailing slashes. It never invents a path.
func NormalizeBaseURL(raw string) string {
	s := strings.Trim(strings.TrimSpace(raw), `"'`)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		host := s
		if i := strings.IndexAny(host, "/?#"); i >= 0 {
			host = host[:i]
		}
		h, _, err := net.SplitHostPort(host)
		if err != nil {
			h = host
		}
		if IsLocalHost(h) {
			s = "http://" + s
		} else {
			s = "https://" + s
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.TrimRight(s, "/")
	}
	u.RawQuery, u.Fragment = "", ""
	p := strings.TrimRight(u.Path, "/")
	for _, route := range []string{"/chat/completions", "/completions", "/responses", "/models"} {
		if strings.HasSuffix(p, route) {
			p = strings.TrimRight(strings.TrimSuffix(p, route), "/")
			break
		}
	}
	u.Path, u.RawPath = p, ""
	return u.String()
}

// IsLocalHost reports whether a host name is this machine or a private
// network address, where a key is usually not needed and http is normal.
func IsLocalHost(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified()
	}
	return false
}

// HostOf returns the host part of a URL without the port.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// KeyEnvFor suggests the environment variable a key for this endpoint should
// live in: api.together.xyz -> TOGETHER_API_KEY. This machine and bare IPs get
// the generic MAK1ZU_API_KEY.
func KeyEnvFor(baseURL string) string {
	host := HostOf(baseURL)
	if host == "" || IsLocalHost(host) || net.ParseIP(host) != nil {
		return "MAK1ZU_API_KEY"
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return "MAK1ZU_API_KEY"
	}
	labels = labels[:len(labels)-1] // the TLD says nothing
	if n := len(labels); n >= 2 {
		switch labels[n-1] {
		case "com", "co", "org", "net", "gov", "edu", "ac":
			labels = labels[:n-1] // x.co.uk
		}
	}
	name := strings.ToUpper(labels[len(labels)-1])
	var b strings.Builder
	for _, r := range name {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "MAK1ZU_API_KEY"
	}
	return b.String() + "_API_KEY"
}

// Custom is the config block for an endpoint nobody has a preset for. Local
// servers get a longer timeout (a model can take a while to load); thinking
// models get room to think before the first visible word.
func Custom(baseURL, keyEnv string, vision bool) config.Provider {
	to := 60
	if IsLocalHost(HostOf(baseURL)) {
		to = 120
	}
	return config.Provider{
		Enabled: true, BaseURL: baseURL, Protocol: "chat", APIKeyEnv: keyEnv,
		TimeoutSeconds: to, CooldownSeconds: 30, Vision: vision, ReasoningRoom: 1000,
	}
}

// Resolved is what ResolveEndpoint learned about an address someone typed.
type Resolved struct {
	BaseURL string
	Models  []string
	Note    string // what was adjusted, in words ("added /v1")
	Err     error  // why the model list could not be read; BaseURL is still usable
	Status  int    // HTTP status of the last try, 0 when nothing answered
}

// ResolveEndpoint normalizes an address and looks at what it serves. When an
// address that does not end in /v1 fails, it retries with /v1 appended (the
// path nearly every server uses) and says so. It never fails outright: a
// server that is not up yet, or that has no /models route, still gets a config.
func ResolveEndpoint(ctx context.Context, raw, key string) Resolved {
	base := NormalizeBaseURL(raw)
	r := Resolved{BaseURL: base}
	if base == "" {
		r.Err = errors.New("empty address")
		return r
	}
	c := config.Provider{BaseURL: base, APIKey: key, TimeoutSeconds: 10}
	ids, status, err := ListModels(ctx, c)
	r.Status = status
	if err == nil {
		r.Models = ids
		return r
	}
	if u, e := url.Parse(base); e == nil && !strings.HasSuffix(u.Path, "/v1") && status != 401 && status != 403 {
		withV1 := config.Provider{BaseURL: base + "/v1", APIKey: key, TimeoutSeconds: 10}
		ids, st2, e2 := ListModels(ctx, withV1)
		switch {
		case e2 == nil:
			r.BaseURL, r.Models, r.Note, r.Status = base+"/v1", ids, "added /v1: that is where this server keeps its API", 200
			return r
		case st2 == 401 || st2 == 403:
			// the route exists and wants a (better) key: the path was right
			r.BaseURL, r.Note, r.Status, r.Err = base+"/v1", "added /v1: that is where this server keeps its API", st2, e2
			return r
		}
	}
	r.Err = err
	return r
}

// Local is a model server found running on this machine.
type Local struct {
	Name    string // best guess from the port; the port is a hint, not a promise
	BaseURL string
	Models  []string
}

var localPorts = []struct {
	Port int
	Name string
}{
	{11434, "Ollama"},
	{1234, "LM Studio"},
	{8080, "llama.cpp / LocalAI"},
	{8000, "vLLM"},
	{1337, "Jan"},
	{5001, "KoboldCpp"},
	{5000, "text-generation-webui"},
	{4000, "LiteLLM"},
}

// DetectLocal looks for OpenAI-compatible servers on this machine's usual
// ports. A port only counts if /v1/models answers with a real model list, so a
// random web app on :8000 is ignored. It takes well under a second.
func DetectLocal(ctx context.Context) []Local {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []Local
	)
	for _, p := range localPorts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := fmt.Sprintf("http://127.0.0.1:%d/v1", p.Port)
			c, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
			defer cancel()
			ids, _, err := ListModels(c, config.Provider{BaseURL: base, TimeoutSeconds: 1})
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, Local{Name: p.Name, BaseURL: base, Models: ids})
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].BaseURL < out[j].BaseURL })
	return out
}
