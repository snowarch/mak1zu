// Package panel is the local control panel: one embedded page, one JSON API.
// It edits the config by path (only what changed), applies it live, tests
// providers with a real call and edits persona files. It is loopback-only by
// default; anything else requires a token.
package panel

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/engine"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
)

//go:embed web
var webFS embed.FS

type Server struct {
	Cfg    *config.Store
	Lib    func() persona.Library
	Home   func() home.Home
	Mem    *memory.Store
	Tel    *telemetry.Log
	Router *provider.Router
	Tools  func() []string
	Ev     *events.Hub
	// Mood describes her current mood; Preview answers a test chat message with
	// the live persona. Both are optional seams wired by main.
	Mood      func() string
	MoodState func() persona.MoodState
	Preview   func(ctx context.Context, speaker string, convo []engine.PreviewTurn) (engine.PreviewResult, error)
	// Avatar returns the pre-rendered expression PNG at a size, when artwork is wired in.
	Avatar  func(name string, size int) ([]byte, bool)
	Version string
	Started time.Time
	// Test seam: build a client for the provider test button.
	NewClient func(name string, p config.Provider) provider.Client
	// Resolve looks at an address someone typed; tests replace it so they never touch the network.
	Resolve func(ctx context.Context, raw, key string) provider.Resolved
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /api/state", s.state)
	mux.HandleFunc("POST /api/config/patch", s.patch)
	mux.HandleFunc("POST /api/provider/test", s.testProvider)
	mux.HandleFunc("GET /api/persona", s.getPersona)
	mux.HandleFunc("PUT /api/persona", s.putPersona)
	mux.HandleFunc("POST /api/persona/activate", s.activate)
	mux.HandleFunc("GET /api/telemetry", s.telemetry)
	mux.HandleFunc("GET /avatar/{name}", s.avatar)
	mux.HandleFunc("GET /api/schema", s.schema)
	mux.HandleFunc("GET /api/presets", s.presets)
	mux.HandleFunc("POST /api/provider/add", s.addProvider)
	mux.HandleFunc("POST /api/provider/discover", s.discover)
	mux.HandleFunc("GET /api/local", s.local)
	mux.HandleFunc("GET /api/events", s.eventsStream)
	mux.HandleFunc("GET /api/events/recent", s.eventsRecent)
	mux.HandleFunc("POST /api/preview", s.preview)
	mux.HandleFunc("GET /api/home/list", s.homeList)
	mux.HandleFunc("GET /api/home/file", s.homeGet)
	mux.HandleFunc("PUT /api/home/file", s.homePut)
	mux.HandleFunc("DELETE /api/home/file", s.homeDelete)
	return s.guard(mux)
}

// guard enforces the panel's threat model: it can write config and call
// paid APIs, so a random web page must never be able to drive it.
//   - Host must be loopback (or the configured host): defeats DNS rebinding.
//   - State-changing requests need the X-Mak1zu header (forces a CORS
//     preflight that we never grant) and a same-origin Origin if present.
//   - A configured token is required for every /api call.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := s.Cfg.Get().WebUI
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !(host == "127.0.0.1" || host == "localhost" || host == "::1" || host == "[::1]" || host == c.Host) {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if c.Token != "" {
				got := r.Header.Get("Authorization")
				want := "Bearer " + c.Token
				if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if r.Header.Get("X-Mak1zu") == "" {
					http.Error(w, "missing X-Mak1zu header", http.StatusForbidden)
					return
				}
				if o := r.Header.Get("Origin"); o != "" && !strings.HasSuffix(o, "//"+r.Host) {
					http.Error(w, "cross-origin", http.StatusForbidden)
					return
				}
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	cool := map[string]float64{}
	if s.Router != nil {
		for k, d := range s.Router.Status() {
			cool[k] = d.Seconds()
		}
	}
	var stats map[string]int
	if s.Mem != nil {
		stats = s.Mem.Stats(r.Context())
	}
	var tools []string
	if s.Tools != nil {
		tools = s.Tools()
	}
	// Providers report whether a key exists, never the key.
	hasKey := map[string]bool{}
	for n, p := range cfg.LLM.Providers {
		hasKey[n] = p.Key() != ""
	}
	mood := ""
	if s.Mood != nil {
		mood = s.Mood()
	}
	var ms persona.MoodState
	if s.MoodState != nil {
		ms = s.MoodState()
	} else {
		ms = persona.MoodState{Name: "neutral"}
	}
	up := int64(0)
	if !s.Started.IsZero() {
		up = int64(time.Since(s.Started).Seconds())
	}
	writeJSON(w, 200, map[string]any{
		"paused": cfg.Behavior.Paused, "mood": mood, "mood_state": ms, "face": Face(ms, cfg.Behavior.Paused, allDown(cfg, cool), s.lastMoment(), time.Now()), "has_avatar": s.Avatar != nil, "uptime_s": up, "version": s.Version,
		"invite_url": InviteURL(cfg.Discord.BotToken(), false), "invite_url_expressions": InviteURL(cfg.Discord.BotToken(), true),
		"checklist": Checklist(cfg), "activity": s.recentActivity(),
		"config": cfg.Redacted(), "personas": s.Lib().List(), "active": cfg.Persona.Active,
		"cooldowns": cool, "memory": stats, "has_key": hasKey, "tools": tools,
		"discord_token_set": cfg.Discord.BotToken() != "",
	})
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Edits map[string]any `json:"edits"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if len(body.Edits) == 0 {
		fail(w, 400, errors.New("no edits"))
		return
	}
	// The panel never edits where its own door is: a typo here would lock
	// the owner out. Change host/port/token in the file.
	for p := range body.Edits {
		if strings.HasPrefix(p, "web_ui.") {
			fail(w, 400, fmt.Errorf("%s can only be changed in the config file", p))
			return
		}
	}
	if err := s.Cfg.Patch(body.Edits); err != nil {
		fail(w, 422, err)
		return
	}
	for p, v := range body.Edits {
		s.note(describeEdit(p, v))
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	p, ok := s.Cfg.Get().LLM.Providers[body.Name]
	if !ok {
		fail(w, 404, errors.New("unknown provider"))
		return
	}
	var cl provider.Client = provider.NewHTTP(body.Name, p)
	if s.NewClient != nil {
		cl = s.NewClient(body.Name, p)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	d := provider.ProbeClient(ctx, cl, p)
	out := map[string]any{"ok": d.OK, "latency_ms": d.Latency.Milliseconds()}
	if d.OK {
		out["reply"] = d.Reply
	} else {
		out["kind"], out["error"], out["fix"], out["models"] = d.Kind, d.Problem, d.Fix, d.Models
	}
	writeJSON(w, 200, out)
}

func (s *Server) getPersona(w http.ResponseWriter, r *http.Request) {
	p, err := s.Lib().Load(r.URL.Query().Get("id"))
	if err != nil {
		fail(w, 404, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": p.ID, "name": p.Name, "text": rawPersona(p)})
}

func rawPersona(p persona.Persona) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	if p.Pronouns != "" {
		fmt.Fprintf(&b, "pronouns: %s\n", p.Pronouns)
	}
	if p.Language != "" {
		fmt.Fprintf(&b, "language: %s\n", p.Language)
	}
	if p.Temperature > 0 {
		fmt.Fprintf(&b, "temperature: %s\n", strconv.FormatFloat(p.Temperature, 'f', -1, 64))
	}
	fmt.Fprintf(&b, "substrate: %t\nmood: %t\n---\n\n%s\n", p.Substrate, p.Mood, p.Body)
	return b.String()
}

func (s *Server) putPersona(w http.ResponseWriter, r *http.Request) {
	var body struct{ ID, Text string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if err := s.Lib().Save(body.ID, body.Text); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) activate(w http.ResponseWriter, r *http.Request) {
	var body struct{ ID string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if _, err := s.Lib().Load(body.ID); err != nil {
		fail(w, 404, err)
		return
	}
	if err := s.Cfg.Patch(map[string]any{"persona.active": body.ID}); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) homeKind(w http.ResponseWriter, r *http.Request) (home.Kind, bool) {
	if s.Home == nil {
		fail(w, 404, errors.New("home not available"))
		return "", false
	}
	k := home.Kind(r.URL.Query().Get("kind"))
	if _, err := s.Home().List(k); err != nil {
		fail(w, 400, err)
		return "", false
	}
	return k, true
}

func (s *Server) homeList(w http.ResponseWriter, r *http.Request) {
	k, ok := s.homeKind(w, r)
	if !ok {
		return
	}
	names, _ := s.Home().List(k)
	writeJSON(w, 200, map[string]any{"kind": k, "names": names})
}

func (s *Server) homeGet(w http.ResponseWriter, r *http.Request) {
	k, ok := s.homeKind(w, r)
	if !ok {
		return
	}
	txt, err := s.Home().Read(k, r.URL.Query().Get("name"))
	if err != nil {
		fail(w, 404, errors.New("not found"))
		return
	}
	writeJSON(w, 200, map[string]any{"text": txt})
}

func (s *Server) homePut(w http.ResponseWriter, r *http.Request) {
	var body struct{ Kind, Name, Text string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if s.Home == nil {
		fail(w, 404, errors.New("home not available"))
		return
	}
	if err := s.Home().Write(home.Kind(body.Kind), body.Name, body.Text); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) homeDelete(w http.ResponseWriter, r *http.Request) {
	k, ok := s.homeKind(w, r)
	if !ok {
		return
	}
	if err := s.Home().Delete(k, r.URL.Query().Get("name")); err != nil {
		fail(w, 422, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) telemetry(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Tel.Recent(100, r.URL.Query().Get("kind")))
}

// Serve listens until ctx ends.
func (s *Server) Serve(ctx context.Context) error {
	c := s.Cfg.Get().WebUI
	srv := &http.Server{Addr: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
