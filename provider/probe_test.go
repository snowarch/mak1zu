package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
)

func probeCfg(url string) config.Provider {
	return config.Provider{Enabled: true, BaseURL: url, Model: "gpt-nano", Protocol: "chat", TimeoutSeconds: 5}
}

func okChat(w http.ResponseWriter) {
	w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
}

func TestProbeOKAndSendsUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		okChat(w)
	}))
	defer srv.Close()
	d := Probe(context.Background(), "p", probeCfg(srv.URL))
	if !d.OK || d.Reply != "ok" {
		t.Fatalf("%+v", d)
	}
	if !strings.HasPrefix(ua, "mak1zu/") {
		t.Fatalf("user agent %q", ua)
	}
}

func TestProbeUserHeadersWin(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ua = r.Header.Get("User-Agent"); okChat(w) }))
	defer srv.Close()
	c := probeCfg(srv.URL)
	c.Headers = map[string]string{"User-Agent": "custom/1"}
	Probe(context.Background(), "p", c)
	if ua != "custom/1" {
		t.Fatalf("user override lost: %q", ua)
	}
}

func TestProbeMissingKeyNeverCallsOut(t *testing.T) {
	c := probeCfg("http://127.0.0.1:1")
	c.APIKeyEnv = "MAK1ZU_TEST_KEY_THAT_IS_NOT_SET"
	d := Probe(context.Background(), "p", c)
	if d.OK || !strings.Contains(d.Problem, "MAK1ZU_TEST_KEY_THAT_IS_NOT_SET") || d.Fix == "" {
		t.Fatalf("%+v", d)
	}
}

func TestProbeExplainsFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"bad key", 401, `{"error":"nope"}`, "rejected the key"},
		{"cloudflare", 403, "error code: 1010", "bot protection"},
		{"quota", 429, `{"error":"slow down"}`, "quota"},
		{"session", 400, `{"error":{"type":"MissingSessionID","message":"missing x-opencode-session"}}`, "session header"},
		{"wrong path", 404, `<html>not found</html>`, "not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			d := Probe(context.Background(), "p", probeCfg(srv.URL))
			if d.OK || !strings.Contains(d.Problem, tc.want) || d.Fix == "" {
				t.Fatalf("%+v", d)
			}
		})
	}
}

func TestProbeSuggestsRealModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Write([]byte(`{"data":[{"id":"zeta"},{"id":"gpt-nano-2"},{"id":"gpt-mini"}]}`))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"message":"model not found"}}`))
	}))
	defer srv.Close()
	d := Probe(context.Background(), "p", probeCfg(srv.URL))
	if d.OK || len(d.Models) == 0 || d.Models[0] != "gpt-nano-2" {
		t.Fatalf("%+v", d)
	}
}

func TestProbeDeadServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	d := Probe(context.Background(), "p", probeCfg(url))
	if d.OK || !strings.Contains(d.Problem, "nothing answered") {
		t.Fatalf("%+v", d)
	}
}

func TestProbeNeverLeaksKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`bad key sk-secret-123`))
	}))
	defer srv.Close()
	c := probeCfg(srv.URL)
	c.APIKey = "sk-secret-123"
	d := Probe(context.Background(), "p", c)
	if strings.Contains(d.Problem+d.Fix, "sk-secret-123") {
		t.Fatalf("key leaked: %+v", d)
	}
}

func TestQuirkHeaders(t *testing.T) {
	if h := quirkHeaders("https://opencode.ai/zen/go/v1"); h["x-opencode-session"] == "" {
		t.Fatal("opencode needs its session header")
	}
	if h := quirkHeaders("https://api.openai.com/v1"); len(h) != 0 {
		t.Fatalf("openai got quirks: %v", h)
	}
}

func TestPresetsAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Presets {
		if p.ID == "" || p.BaseURL == "" || p.Model == "" || (p.Protocol != "chat" && p.Protocol != "responses") {
			t.Errorf("incomplete preset %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate %s", p.ID)
		}
		seen[p.ID] = true
		if c := p.Provider(); !c.Enabled || c.BaseURL != p.BaseURL {
			t.Errorf("bad conversion %s", p.ID)
		}
	}
}
