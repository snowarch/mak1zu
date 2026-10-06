package panel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/engine"
	"github.com/snowarch/mak1zu/internal/events"
	"github.com/snowarch/mak1zu/provider"
)

// Step is one line of the first-run checklist.
type Step struct {
	ID   string `json:"id"`
	OK   bool   `json:"ok"`
	Text string `json:"text"`
	Fix  string `json:"fix,omitempty"`
	Tab  string `json:"tab,omitempty"` // where to go to fix it
}

// Checklist tells a newcomer what is missing before she can really live
// somewhere. It looks only at config and environment, never makes a call.
func Checklist(cfg config.Config) []Step {
	var steps []Step
	routed := cfg.LLM.Routing.Text
	modelOK, why := false, "no model is routed to answer chat"
	for _, n := range routed {
		p, ok := cfg.LLM.Providers[n]
		if !ok || !p.Enabled {
			continue
		}
		if p.APIKeyEnv != "" && p.Key() == "" {
			why = fmt.Sprintf("%s has no key: set %s in .makizu/.env", n, p.APIKeyEnv)
			continue
		}
		modelOK = true
	}
	steps = append(steps, Step{ID: "model", OK: modelOK, Text: "A model she can think with", Fix: why, Tab: "models"})
	if cfg.Discord.Enabled {
		steps = append(steps,
			Step{ID: "token", OK: cfg.Discord.BotToken() != "", Text: "Discord bot token", Fix: "put " + orDefault(cfg.Discord.TokenEnv, "MAK1ZU_DISCORD_TOKEN") + " in .makizu/.env, then restart", Tab: "rooms"},
			Step{ID: "owner", OK: cfg.Discord.OwnerID != "", Text: "You as her owner", Fix: "without an owner ID anyone can DM her and use owner-only commands", Tab: "rooms"},
			Step{ID: "home", OK: len(cfg.Discord.HomeChannels) > 0, Text: "A home channel", Fix: "without one she only answers when called and never joins in", Tab: "rooms"},
		)
	} else {
		steps = append(steps, Step{ID: "discord", OK: false, Text: "Discord is off", Fix: "she is terminal-only until you turn Discord on under Rooms", Tab: "rooms"})
	}
	for i := range steps {
		if steps[i].OK {
			steps[i].Fix = ""
		}
	}
	return steps
}

// invitePerms is what she needs in a server: read and write messages, threads,
// embeds, files, history, reactions, external emoji and slash commands.
const invitePerms = 277025770560

// InviteURL builds the "add her to a server" link from the bot token alone: the
// token's first segment is the base64 of the bot's own user ID. It returns ""
// when the token does not look like one. The token itself is never echoed.
func InviteURL(token string) string {
	seg, _, ok := strings.Cut(token, ".")
	if !ok {
		return ""
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(seg, "="))
	if err != nil || len(raw) < 15 || len(raw) > 21 {
		return ""
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s&scope=bot%%20applications.commands&permissions=%d", raw, invitePerms)
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

type activity struct {
	Heard    int   `json:"heard"`
	Replied  int   `json:"replied"`
	Quiet    int   `json:"quiet"`
	Incident int   `json:"incidents"`
	LastTS   int64 `json:"last_reply_ms,omitempty"`
}

// recentActivity counts the last hour of the feed.
func (s *Server) recentActivity() activity {
	var a activity
	if s.Ev == nil {
		return a
	}
	cut := time.Now().Add(-time.Hour)
	for _, e := range s.Ev.Since(0) {
		if e.TS.Before(cut) {
			continue
		}
		switch e.Type {
		case "heard":
			a.Heard++
		case "replied":
			a.Replied++
			a.LastTS = e.TS.UnixMilli()
		case "quiet":
			a.Quiet++
		case "incident":
			a.Incident++
		}
	}
	return a
}

func (s *Server) schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"groups": Groups, "settings": schemaWithDefaults()})
}

func (s *Server) presets(w http.ResponseWriter, r *http.Request) {
	type row struct {
		provider.Preset
		KeyFound bool `json:"key_found"`
	}
	var out []row
	for _, p := range provider.Presets {
		out = append(out, row{p, p.KeyEnv != "" && os.Getenv(p.KeyEnv) != ""})
	}
	writeJSON(w, 200, out)
}

var providerName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func (s *Server) addProvider(w http.ResponseWriter, r *http.Request) {
	var body struct{ Preset, Name string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	pr, ok := provider.PresetByID(body.Preset)
	if !ok {
		fail(w, 404, errors.New("unknown preset"))
		return
	}
	name := strings.ToLower(strings.TrimSpace(body.Name))
	if name == "" {
		name = pr.ID
	}
	if !providerName.MatchString(name) {
		fail(w, 422, errors.New("name: lowercase letters, digits, - and _"))
		return
	}
	if _, exists := s.Cfg.Get().LLM.Providers[name]; exists {
		fail(w, 409, fmt.Errorf("%q already exists", name))
		return
	}
	pj, _ := json.Marshal(pr.Provider())
	var v map[string]any
	_ = json.Unmarshal(pj, &v)
	if err := s.Cfg.Patch(map[string]any{"llm.providers." + name: v}); err != nil {
		fail(w, 422, err)
		return
	}
	s.note("added provider " + name + " from the " + pr.Label + " preset")
	writeJSON(w, 200, map[string]any{"ok": true, "name": name})
}

// note puts a panel action in the feed so changes are visible as they land.
func (s *Server) note(msg string) {
	s.Ev.Emit(events.Event{Type: "system", Why: msg})
}

var secretish = regexp.MustCompile(`(?i)key|token|secret|password`)

// describeEdit renders one config change for the feed without ever printing a secret.
func describeEdit(path string, v any) string {
	label := path
	for _, c := range Catalog {
		if c.Path == path {
			label = c.Label
			break
		}
	}
	if secretish.MatchString(path) {
		return label + " changed"
	}
	b, _ := json.Marshal(v)
	val := string(b)
	if len(val) > 70 {
		val = val[:70] + "…"
	}
	return label + " → " + val
}

// events streams the live feed as server-sent events. The panel reads it with
// fetch so the auth header still works.
func (s *Server) eventsStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok || s.Ev == nil {
		fail(w, 500, errors.New("streaming unavailable"))
		return
	}
	since, _ := strconv.ParseInt(firstNonEmpty(r.URL.Query().Get("since"), r.Header.Get("Last-Event-ID")), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, cancel := s.Ev.Subscribe()
	defer cancel()
	last := since
	send := func(e events.Event) {
		if e.ID <= last {
			return
		}
		last = e.ID
		b, _ := json.Marshal(e)
		fmt.Fprintf(w, "id: %d\ndata: %s\n\n", e.ID, b)
	}
	for _, e := range s.Ev.Since(since) {
		send(e)
	}
	fl.Flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			send(e)
			fl.Flush()
		case <-tick.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			fl.Flush()
		}
	}
}

func (s *Server) eventsRecent(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	if s.Ev == nil {
		writeJSON(w, 200, []events.Event{})
		return
	}
	writeJSON(w, 200, s.Ev.Since(since))
}

func firstNonEmpty(a ...string) string {
	for _, x := range a {
		if x != "" {
			return x
		}
	}
	return ""
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	if s.Preview == nil {
		fail(w, 404, errors.New("preview is not available in this build"))
		return
	}
	var body struct {
		Speaker string               `json:"speaker"`
		Convo   []engine.PreviewTurn `json:"convo"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<17)).Decode(&body); err != nil {
		fail(w, 400, err)
		return
	}
	if len(body.Convo) > 40 {
		body.Convo = body.Convo[len(body.Convo)-40:]
	}
	res, err := s.Preview(r.Context(), body.Speaker, body.Convo)
	if err != nil {
		writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error(), "system": res.System})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "result": res})
}
