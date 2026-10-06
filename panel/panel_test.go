package panel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
)

func newServer(t *testing.T) (*Server, string) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "personas", "maki"), 0o755)
	os.WriteFile(filepath.Join(dir, "personas", "maki", "persona.md"), []byte("---\nname: Maki\n---\nbody"), 0o644)
	cfg := config.Default()
	cfg.Persona.Dir = filepath.Join(dir, "personas")
	cfg.LLM.Providers["main"] = config.Provider{Enabled: true, BaseURL: "http://x", Model: "m", APIKey: "sk-TOPSECRET"}
	path := filepath.Join(dir, "config.json")
	st := config.New(path, cfg)
	st.Save(cfg)
	hm := func() home.Home { return home.Home{Dir: dir} }
	return &Server{Cfg: st, Home: hm, Lib: func() persona.Library { return persona.Library{Dir: cfg.Persona.Dir} }, Tel: telemetry.New(10, "")}, path
}

func do(s *Server, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:8787"
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

var csrf = map[string]string{"X-Mak1zu": "1"}

func TestStateNeverContainsSecrets(t *testing.T) {
	s, _ := newServer(t)
	w := do(s, "GET", "/api/state", "", nil)
	if strings.Contains(w.Body.String(), "TOPSECRET") {
		t.Fatal("api key leaked through /api/state")
	}
	var v struct {
		HasKey map[string]bool `json:"has_key"`
	}
	json.Unmarshal(w.Body.Bytes(), &v)
	if !v.HasKey["main"] {
		t.Fatal("panel must still know a key exists")
	}
}

func TestPatchAppliesOnlyWhatChangedAndEchoedMaskKeepsSecret(t *testing.T) {
	s, path := newServer(t)
	// A hand edit lands while the tab is open.
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), `"temperature": 0.9`, `"temperature": 1.3`, 1))
	os.WriteFile(path, b, 0o600)
	w := do(s, "POST", "/api/config/patch", `{"edits":{"behavior.turn.max_tokens":333,"llm.providers.main.api_key":"***"}}`, csrf)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	got := s.Cfg.Get()
	if got.Behavior.Turn.MaxTokens != 333 {
		t.Fatal("edit not applied")
	}
	if got.LLM.Temperature != 1.3 {
		t.Fatal("stale tab overwrote a hand edit")
	}
	if got.LLM.Providers["main"].APIKey != "sk-TOPSECRET" {
		t.Fatal("masked secret overwrote the real one")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("config perms %v", st.Mode().Perm())
	}
}

func TestPatchRejectsInvalidAndDoorEdits(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "POST", "/api/config/patch", `{"edits":{"llm.routing.text":["ghost"]}}`, csrf); w.Code != 422 {
		t.Fatalf("routing to unknown provider accepted: %d", w.Code)
	}
	if w := do(s, "POST", "/api/config/patch", `{"edits":{"web_ui.port":1}}`, csrf); w.Code != 400 {
		t.Fatal("panel edited its own door")
	}
	if s.Cfg.Get().LLM.Routing.Text[0] != "main" {
		t.Fatal("invalid patch partially applied")
	}
}

func TestCSRFAndRebindingGuards(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "POST", "/api/config/patch", `{"edits":{"name":"x"}}`, nil); w.Code != http.StatusForbidden {
		t.Fatal("POST without X-Mak1zu header accepted")
	}
	if w := do(s, "POST", "/api/config/patch", `{"edits":{"name":"x"}}`, map[string]string{"X-Mak1zu": "1", "Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
		t.Fatal("cross-origin POST accepted")
	}
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Host = "evil.example"
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("DNS rebinding host accepted")
	}
}

func TestTokenRequiredWhenSet(t *testing.T) {
	s, _ := newServer(t)
	s.Cfg.Patch(map[string]any{"name": "n"})
	cfg := s.Cfg.Get()
	cfg.WebUI.Token = "tok"
	s.Cfg = config.New(s.Cfg.Path(), cfg)
	if w := do(s, "GET", "/api/state", "", nil); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := do(s, "GET", "/api/state", "", map[string]string{"Authorization": "Bearer tok"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

type fakeClient struct{ err error }

func (f fakeClient) Complete(context.Context, provider.Request) (provider.Response, error) {
	return provider.Response{Text: "pong"}, f.err
}

func TestProviderTestIsARealCallAndReportsKind(t *testing.T) {
	s, _ := newServer(t)
	s.NewClient = func(string, config.Provider) provider.Client {
		return fakeClient{&provider.Error{Kind: provider.KindAuth, Provider: "main", Status: 401, Msg: "bad key"}}
	}
	w := do(s, "POST", "/api/provider/test", `{"name":"main"}`, csrf)
	var v map[string]any
	json.Unmarshal(w.Body.Bytes(), &v)
	if v["ok"] != false || v["kind"] != "auth" {
		t.Fatal(v)
	}
}

func TestPersonaRoundTripAndPathTraversal(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "PUT", "/api/persona", `{"id":"../evil","text":"---\nname: x\n---\nbody"}`, csrf); w.Code != 422 {
		t.Fatal("path traversal accepted")
	}
	if w := do(s, "PUT", "/api/persona", `{"id":"rin","text":"---\nname: Rin\n---\nYou are Rin."}`, csrf); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := do(s, "POST", "/api/persona/activate", `{"id":"rin"}`, csrf); w.Code != 200 || s.Cfg.Get().Persona.Active != "rin" {
		t.Fatal("activate failed")
	}
	if w := do(s, "POST", "/api/persona/activate", `{"id":"ghost"}`, csrf); w.Code != 404 {
		t.Fatal("activated a missing persona")
	}
}

func TestDirectivesAPIRoundTripAndTraversal(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "PUT", "/api/home/file", `{"kind":"rules","name":"tone","text":"Be dry."}`, csrf); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := do(s, "GET", "/api/home/file?kind=rules&name=tone", "", nil)
	if !strings.Contains(w.Body.String(), "Be dry.") {
		t.Fatal(w.Body.String())
	}
	for _, bad := range []string{
		`{"kind":"rules","name":"../config","text":"x"}`,
		`{"kind":"skills","name":"../../x","text":"x"}`,
		`{"kind":"nope","name":"a","text":"x"}`,
		`{"kind":"servers","name":"abc","text":"x"}`,
	} {
		if w := do(s, "PUT", "/api/home/file", bad, csrf); w.Code == 200 {
			t.Errorf("accepted %s", bad)
		}
	}
	if w := do(s, "GET", "/api/home/file?kind=rules&name=../config", "", nil); w.Code == 200 {
		t.Fatal("traversal read")
	}
	if w := do(s, "PUT", "/api/home/file", `{"kind":"rules","name":"x","text":"y"}`, nil); w.Code != http.StatusForbidden {
		t.Fatal("write without CSRF header accepted")
	}
	if w := do(s, "DELETE", "/api/home/file?kind=rules&name=tone", "", csrf); w.Code != 200 {
		t.Fatal(w.Code)
	}
}
