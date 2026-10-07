package panel

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	u := InviteURL(tok, false)
	if !strings.Contains(u, "client_id=123456789012345678") || !strings.Contains(u, "applications.commands") {
		t.Fatal(u)
	}
	if strings.Contains(u, "notarealsecret") || strings.Contains(u, "AAAAAA") {
		t.Fatal("token material in the link")
	}
	for _, bad := range []string{"", "nope", "abc.def.ghi", "MTIz.x.y"} {
		if InviteURL(bad, false) != "" {
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

func TestExpressionPermissionsAreOptIn(t *testing.T) {
	tok := "MTIzNDU2Nzg5MDEyMzQ1Njc4.AAAAAA.x"
	base, wide := InviteURL(tok, false), InviteURL(tok, true)
	if base == wide {
		t.Fatal("expressions must change the link")
	}
	if !strings.Contains(wide, "permissions=9074192534592") { // base | create | manage expressions
		t.Fatal(wide)
	}
	if strings.Contains(base, "9074192534592") {
		t.Fatal("default link carries the broad permissions")
	}
}

func TestFaceFollowsMoodThenMoments(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	mood := func(n string, i float64) persona.MoodState { return persona.MoodState{Name: n, Intensity: i} }
	ev := func(typ, reason string, ago time.Duration) *events.Event {
		return &events.Event{Type: typ, Reason: reason, TS: now.Add(-ago)}
	}
	cases := []struct {
		name   string
		m      persona.MoodState
		paused bool
		last   *events.Event
		want   string
	}{
		{"baseline", mood("neutral", 0), false, nil, "neutral"},
		{"amusement", mood("amused", 0.4), false, nil, "amused"},
		{"smug", mood("smug", 0.9), false, nil, "smug"},
		{"flustered shows as embarrassed", mood("flustered", 0.8), false, nil, "embarrassed"},
		{"mild irritation is deadpan", mood("irritated", 0.4), false, nil, "deadpan"},
		{"strong irritation", mood("irritated", 0.9), false, nil, "irritated"},
		{"sleepy", mood("sleepy", 0.5), false, nil, "sleepy"},
		{"wired", mood("wired", 0.5), false, nil, "wired"},
		{"paused sleeps", mood("wired", 1), true, nil, "sleepy"},
		{"fresh incident", mood("neutral", 0), false, ev("incident", "misconfigured", 2*time.Second), "alarm"},
		{"old incident is forgotten", mood("neutral", 0), false, ev("incident", "misconfigured", time.Minute), "neutral"},
		{"incident beats pause", mood("neutral", 0), true, ev("incident", "x", time.Second), "alarm"},
		{"just replied", mood("neutral", 0), false, ev("replied", "", time.Second), "amused"},
		{"chose not to answer", mood("neutral", 0), false, ev("quiet", "dice", time.Second), "deadpan"},
	}
	for _, tc := range cases {
		if got := Face(tc.m, tc.paused, false, tc.last, now); got != tc.want {
			t.Errorf("%s: got %s want %s", tc.name, got, tc.want)
		}
	}
	night := time.Date(2026, 10, 6, 3, 0, 0, 0, time.UTC)
	if got := Face(mood("neutral", 0), false, false, &events.Event{Type: "heard", Reason: "mention", TS: night.Add(-time.Second)}, night); got != "embarrassed" {
		t.Errorf("woken at night: %s", got)
	}
}

func TestFaceOnlyReturnsContractNames(t *testing.T) {
	ok := map[string]bool{}
	for _, n := range FaceNames {
		ok[n] = true
	}
	now := time.Now()
	for _, n := range []string{"neutral", "amused", "smug", "flustered", "irritated", "sleepy", "wired", "bogus"} {
		for _, in := range []float64{0, 0.5, 1} {
			for _, p := range []bool{false, true} {
				if f := Face(persona.MoodState{Name: n, Intensity: in}, p, p, nil, now); !ok[f] {
					t.Fatalf("%s/%v/%v -> %q is not in the contract", n, in, p, f)
				}
			}
		}
	}
}

func TestAvatarRouteIs404UntilArtIsWired(t *testing.T) {
	s, _ := newServer(t)
	if w := do(s, "GET", "/avatar/neutral.png", "", nil); w.Code != 404 {
		t.Fatalf("no art wired yet, got %d", w.Code)
	}
	s.Avatar = func(name string, size int) ([]byte, bool) {
		if name == "smug" {
			return []byte("PNG" + strconv.Itoa(size)), true
		}
		return nil, false
	}
	if w := do(s, "GET", "/avatar/smug.png?size=96", "", nil); w.Code != 200 || w.Body.String() != "PNG96" || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
	for _, p := range []string{"/avatar/evil.png", "/avatar/..%2fsecret", "/avatar/shy.png"} {
		if w := do(s, "GET", p, "", nil); w.Code != 404 {
			t.Errorf("%s -> %d", p, w.Code)
		}
	}
}

func TestFaceNewFacesAndSadness(t *testing.T) {
	now := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC)
	m := persona.MoodState{Name: "neutral"}
	ev := func(typ string, first bool, ago time.Duration) *events.Event {
		return &events.Event{Type: typ, First: first, Reason: "mention", TS: now.Add(-ago)}
	}
	if f := Face(m, false, false, ev("heard", true, time.Second), now); f != "shy" {
		t.Errorf("stranger: %s", f)
	}
	if f := Face(m, false, false, ev("heard", true, 10*time.Second), now); f != "neutral" {
		t.Errorf("shyness should pass: %s", f)
	}
	if f := Face(m, false, false, ev("slip", false, time.Second), now); f != "waitwait" {
		t.Errorf("caught herself: %s", f)
	}
	if f := Face(m, false, true, nil, now); f != "sad" {
		t.Errorf("all models down: %s", f)
	}
	if f := Face(m, false, false, nil, now); f != "neutral" {
		t.Errorf("recovered should clear sadness: %s", f)
	}
}

func TestAllDownNeedsEveryRoutedProviderCooling(t *testing.T) {
	cfg := config.Default()
	cfg.LLM.Providers["b"] = config.Provider{Enabled: true}
	cfg.LLM.Routing.Text = []string{"main", "b"}
	if allDown(cfg, map[string]float64{"main": 20}) {
		t.Fatal("b is healthy, she can still think")
	}
	if !allDown(cfg, map[string]float64{"main": 20, "b": 5}) {
		t.Fatal("both cooling means down")
	}
	cfg.LLM.Routing.Text = nil
	if allDown(cfg, nil) {
		t.Fatal("no providers is a setup problem, not sadness")
	}
}

func fakeModelServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Write([]byte(`{"data":[{"id":"alpha"},{"id":"beta"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverNormalizesAndListsModels(t *testing.T) {
	s, _ := newServer(t)
	srv := fakeModelServer(t)
	w := do(s, "POST", "/api/provider/discover", `{"base_url":"`+srv.URL+`/v1/chat/completions"}`, csrf)
	var got struct {
		BaseURL string   `json:"base_url"`
		Models  []string `json:"models"`
		Local   bool     `json:"local"`
		Error   string   `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	if w.Code != 200 || got.BaseURL != srv.URL+"/v1" || len(got.Models) != 2 || !got.Local || got.Error != "" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = do(s, "POST", "/api/provider/discover", `{"base_url":"`+srv.URL+`"}`, csrf)
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.BaseURL != srv.URL+"/v1" {
		t.Fatalf("a bare host should find /v1: %s", w.Body)
	}
	if w := do(s, "POST", "/api/provider/discover", `{"base_url":"http://127.0.0.1:1/v1"}`, csrf); !strings.Contains(w.Body.String(), `"error"`) || w.Code != 200 {
		t.Fatalf("nothing answering is a result, not a failure: %d %s", w.Code, w.Body)
	}
	if w := do(s, "POST", "/api/provider/discover", `{}`, csrf); w.Code != 422 {
		t.Fatalf("%d", w.Code)
	}
}

func TestAddCustomEndpoint(t *testing.T) {
	s, _ := newServer(t)
	s.Resolve = func(_ context.Context, raw, _ string) provider.Resolved {
		return provider.Resolved{BaseURL: provider.NormalizeBaseURL(raw), Err: errors.New("offline")}
	}
	body := `{"preset":"custom","base_url":"api.together.xyz/v1/chat/completions","model":"m1","key":"sk-PANELSECRET","vision":true}`
	w := do(s, "POST", "/api/provider/add", body, csrf)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	p := s.Cfg.Get().LLM.Providers["together"]
	if p.BaseURL != "https://api.together.xyz/v1" || p.Model != "m1" || !p.Vision || p.APIKey != "sk-PANELSECRET" || p.APIKeyEnv != "" || p.Protocol != "chat" {
		t.Fatalf("%+v", p)
	}
	if strings.Contains(w.Body.String(), "PANELSECRET") {
		t.Fatal("key echoed")
	}
	if w := do(s, "GET", "/api/state", "", nil); strings.Contains(w.Body.String(), "PANELSECRET") {
		t.Fatal("state leaks the key")
	}
	w = do(s, "POST", "/api/provider/add", `{"preset":"custom","base_url":"http://localhost:8000/v1","model":"q"}`, csrf)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if lp := s.Cfg.Get().LLM.Providers["local"]; lp.APIKeyEnv != "" || lp.TimeoutSeconds != 120 {
		t.Fatalf("a local server needs no key: %+v", lp)
	}
	for body, code := range map[string]int{
		`{"preset":"custom","model":"x"}`:                                               422,
		`{"preset":"custom","base_url":"http://x/v1"}`:                                  422,
		`{"preset":"custom","base_url":"http://x/v1","model":"m","key_env":"bad name"}`: 422,
		`{"preset":"custom","base_url":"https://api.together.xyz/v1","model":"m"}`:      409,
	} {
		if w := do(s, "POST", "/api/provider/add", body, csrf); w.Code != code {
			t.Errorf("%s -> %d, want %d", body, w.Code, code)
		}
	}
}

func TestAddCustomWithoutLookStillFindsV1(t *testing.T) {
	s, _ := newServer(t)
	srv := fakeModelServer(t)
	body := `{"preset":"custom","name":"fresh","base_url":"` + srv.URL + `","model":"alpha"}`
	if w := do(s, "POST", "/api/provider/add", body, csrf); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := s.Cfg.Get().LLM.Providers["fresh"].BaseURL; got != srv.URL+"/v1" {
		t.Fatalf("base_url %q", got)
	}
}

func TestLocalEndpointAnswersWithAList(t *testing.T) {
	s, _ := newServer(t)
	w := do(s, "GET", "/api/local", "", nil)
	if w.Code != 200 || !strings.HasPrefix(strings.TrimSpace(w.Body.String()), "[") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
