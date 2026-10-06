package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/engine"
	"github.com/snowarch/mak1zu/internal/events"
)

func TestSchemaServesGroupsAndDefaults(t *testing.T) {
	s, _ := newServer(t)
	var v struct {
		Groups   []Group   `json:"groups"`
		Settings []Setting `json:"settings"`
	}
	json.Unmarshal(do(s, "GET", "/api/schema", "", nil).Body.Bytes(), &v)
	if len(v.Groups) == 0 || len(v.Settings) < 30 {
		t.Fatalf("%d groups, %d settings", len(v.Groups), len(v.Settings))
	}
	for _, st := range v.Settings {
		if st.Path == "behavior.response.chances.mentioned" && st.Default != 1.0 {
			t.Fatalf("default not filled: %+v", st)
		}
	}
}

func TestPresetsEndpointNeverLeaksKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-SHOULD-NOT-APPEAR")
	s, _ := newServer(t)
	w := do(s, "GET", "/api/presets", "", nil)
	if strings.Contains(w.Body.String(), "SHOULD-NOT-APPEAR") {
		t.Fatal("key value leaked")
	}
	var rows []struct {
		ID       string `json:"ID"`
		KeyFound bool   `json:"key_found"`
	}
	json.Unmarshal(w.Body.Bytes(), &rows)
	found := false
	for _, r := range rows {
		if r.ID == "openai" {
			found = r.KeyFound
		}
	}
	if !found {
		t.Fatalf("panel should say the key is present: %s", w.Body.String())
	}
}

func TestAddProviderFromPreset(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "POST", "/api/provider/add", `{"preset":"ollama","name":"local"}`, csrf); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p := s.Cfg.Get().LLM.Providers["local"]
	if p.BaseURL != "http://localhost:11434/v1" || !p.Enabled || p.APIKeyEnv != "" {
		t.Fatalf("%+v", p)
	}
	for body, code := range map[string]int{
		`{"preset":"ollama","name":"local"}`:    409, // exists
		`{"preset":"nope"}`:                     404,
		`{"preset":"ollama","name":"Bad Name"}`: 422,
	} {
		if w := do(s, "POST", "/api/provider/add", body, csrf); w.Code != code {
			t.Errorf("%s -> %d, want %d", body, w.Code, code)
		}
	}
}

func TestPatchShowsUpInTheFeedWithoutSecrets(t *testing.T) {
	s, _ := newServer(t)
	s.Ev = events.NewHub(10)
	do(s, "POST", "/api/config/patch", `{"edits":{"behavior.response.chances.interesting_home":0.05,"llm.providers.main.api_key":"sk-NEWSECRET"}}`, csrf)
	var all strings.Builder
	for _, e := range s.Ev.Since(0) {
		all.WriteString(e.Type + " " + e.Why + "\n")
	}
	if !strings.Contains(all.String(), "Idle chatter in a home channel → 0.05") {
		t.Fatalf("change not announced:\n%s", all.String())
	}
	if strings.Contains(all.String(), "NEWSECRET") {
		t.Fatal("secret reached the feed")
	}
}

func TestStateCarriesPauseMoodAndChecklist(t *testing.T) {
	s, _ := newServer(t)
	s.Mood = func() string { return "a bit bored" }
	s.Ev = events.NewHub(10)
	s.Ev.Emit(events.Event{Type: "replied"})
	s.Ev.Emit(events.Event{Type: "quiet"})
	s.Cfg.Patch(map[string]any{"behavior.paused": true})
	var v struct {
		Paused    bool     `json:"paused"`
		Mood      string   `json:"mood"`
		Checklist []Step   `json:"checklist"`
		Activity  activity `json:"activity"`
	}
	json.Unmarshal(do(s, "GET", "/api/state", "", nil).Body.Bytes(), &v)
	if !v.Paused || v.Mood != "a bit bored" || len(v.Checklist) == 0 || v.Activity.Replied != 1 || v.Activity.Quiet != 1 {
		t.Fatalf("%+v", v)
	}
}

func TestChecklistTellsNewcomersWhatIsMissing(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.Enabled = true
	t.Setenv("MAK1ZU_API_KEY", "")
	t.Setenv("MAK1ZU_DISCORD_TOKEN", "")
	by := map[string]Step{}
	for _, st := range Checklist(cfg) {
		by[st.ID] = st
	}
	if by["model"].OK || !strings.Contains(by["model"].Fix, "MAK1ZU_API_KEY") {
		t.Fatalf("model: %+v", by["model"])
	}
	if by["token"].OK || by["owner"].OK || by["home"].OK {
		t.Fatalf("%+v", by)
	}
	t.Setenv("MAK1ZU_API_KEY", "x")
	t.Setenv("MAK1ZU_DISCORD_TOKEN", "y")
	cfg.Discord.OwnerID, cfg.Discord.HomeChannels = "1", []string{"2"}
	for _, st := range Checklist(cfg) {
		if !st.OK {
			t.Errorf("%s should pass now: %+v", st.ID, st)
		}
	}
}

func TestPreviewEndpointUsesTheSeam(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "POST", "/api/preview", `{"convo":[]}`, csrf); w.Code != 404 {
		t.Fatalf("no seam should be 404, got %d", w.Code)
	}
	s.Preview = func(_ context.Context, speaker string, c []engine.PreviewTurn) (engine.PreviewResult, error) {
		return engine.PreviewResult{Reply: "hi " + speaker + " x" + string(rune('0'+len(c))), System: "SYS"}, nil
	}
	w := do(s, "POST", "/api/preview", `{"speaker":"Dana","convo":[{"role":"you","text":"hello"}]}`, csrf)
	if !strings.Contains(w.Body.String(), "hi Dana x1") {
		t.Fatal(w.Body.String())
	}
}

func TestEventStreamReplaysThenGoesLive(t *testing.T) {
	s, _ := newServer(t)
	s.Ev = events.NewHub(10)
	s.Ev.Emit(events.Event{Type: "heard", Author: "old"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal(resp.Header)
	}
	rd := bufio.NewReader(resp.Body)
	next := func() events.Event {
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(line, "data: ") {
				var e events.Event
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e)
				return e
			}
		}
	}
	if e := next(); e.Author != "old" {
		t.Fatalf("replay: %+v", e)
	}
	go func() { time.Sleep(100 * time.Millisecond); s.Ev.Emit(events.Event{Type: "replied", Text: "live!"}) }()
	if e := next(); e.Text != "live!" {
		t.Fatalf("live: %+v", e)
	}
}

func TestInviteURLComesFromTheTokenAndNeverEchoesIt(t *testing.T) {
	tok := "MTIzNDU2Nzg5MDEyMzQ1Njc4.AAAAAA.notarealsecret"
	u := InviteURL(tok)
	if !strings.Contains(u, "client_id=123456789012345678") || !strings.Contains(u, "applications.commands") {
		t.Fatal(u)
	}
	if strings.Contains(u, "notarealsecret") || strings.Contains(u, "AAAAAA") {
		t.Fatal("token material in the link")
	}
	for _, bad := range []string{"", "nope", "abc.def.ghi", "MTIz.x.y"} {
		if InviteURL(bad) != "" {
			t.Errorf("%q should not produce a link", bad)
		}
	}
}

func TestPassingStepsCarryNoFixText(t *testing.T) {
	cfg := config.Default()
	t.Setenv("MAK1ZU_API_KEY", "x")
	for _, st := range Checklist(cfg) {
		if st.OK && st.Fix != "" {
			t.Errorf("%s passes but still says %q", st.ID, st.Fix)
		}
	}
}

func TestEveryAssetThePageReferencesIsServed(t *testing.T) {
	s, _ := newServer(t)
	page := do(s, "GET", "/", "", nil).Body.String()
	for _, want := range []string{"app.css", "app.js", "logo.svg", "favicon.png"} {
		if !strings.Contains(page, want) {
			t.Fatalf("index.html no longer references %s", want)
		}
		w := do(s, "GET", "/"+want, "", nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("%s: %d, %d bytes", want, w.Code, w.Body.Len())
		}
	}
}
