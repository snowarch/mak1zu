// Package config owns the single settings file. The panel edits it by sending
// only the paths that changed; Patch applies them to what is on disk so a
// stale browser tab or a hand edit is never overwritten.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type Provider struct {
	Enabled         bool              `json:"enabled"`
	BaseURL         string            `json:"base_url"`
	Model           string            `json:"model"`
	Protocol        string            `json:"protocol"` // "chat" (default) or "responses"
	APIKeyEnv       string            `json:"api_key_env,omitempty"`
	APIKey          string            `json:"api_key,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	ReasoningRoom   int               `json:"reasoning_headroom,omitempty"` // extra completion tokens for thinking models
	TimeoutSeconds  int               `json:"timeout_seconds,omitempty"`
	CooldownSeconds int               `json:"failure_cooldown_seconds,omitempty"`
	Vision          bool              `json:"vision,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"`
	ExtraBody       map[string]any    `json:"extra_body,omitempty"`
}

type Routing struct {
	Text   []string `json:"text"`
	Vision []string `json:"vision"`
}

type LLM struct {
	Temperature float64             `json:"temperature"`
	Routing     Routing             `json:"routing"`
	Providers   map[string]Provider `json:"providers"`
}

type Chances struct {
	Mentioned     float64 `json:"mentioned"`
	ReplyToHer    float64 `json:"reply_to_her"`
	NameInMessage float64 `json:"name_in_message"`
	Question      float64 `json:"question_home"`
	ActiveConvo   float64 `json:"active_convo"`
	Interesting   float64 `json:"interesting_home"`
	Peer          float64 `json:"peer"`
}

type Response struct {
	MaxPerMinute     int     `json:"max_per_minute"`
	AmbientCooldown  float64 `json:"ambient_cooldown_seconds"`
	RecentActiveSecs float64 `json:"recent_active_seconds"`
	HomeAlwaysReply  bool    `json:"home_always_reply"`
	BurstSettleSecs  float64 `json:"burst_settle_seconds"`
	PeerCooldownSecs float64 `json:"peer_cooldown_seconds"`
	MaxPeerExchanges int     `json:"max_peer_exchanges"`
	Chances          Chances `json:"chances"`
}

type Timing struct {
	ThinkMin    float64 `json:"think_min"`
	ThinkMax    float64 `json:"think_max"`
	TypingPerCh float64 `json:"typing_seconds_per_char"`
	TypingMax   float64 `json:"typing_max_seconds"`
}

type Turn struct {
	MaxRounds     int `json:"max_rounds"`
	MaxTokens     int `json:"max_tokens"`
	HeavyTokens   int `json:"heavy_tokens"`
	BudgetSeconds int `json:"tool_turn_budget_seconds"`
	HistoryLimit  int `json:"history_limit"`
	MaxReplyChars int `json:"max_reply_chars"`
	EmojiBudget   int `json:"emoji_budget"`
}

type Behavior struct {
	Response Response `json:"response"`
	Timing   Timing   `json:"timing"`
	Turn     Turn     `json:"turn"`
	Retry    bool     `json:"retry_transient"`
	// Paused is the kill switch: she hears everything and answers nothing.
	Paused bool `json:"paused"`
}

type Persona struct {
	Active string `json:"active"`
	Dir    string `json:"dir"`
}

type Discord struct {
	Enabled  bool   `json:"enabled"`
	TokenEnv string `json:"token_env,omitempty"`
	Token    string `json:"token,omitempty"`
	OwnerID  string `json:"owner_id"`
	// Discord IDs are strings everywhere: JS numbers corrupt snowflakes.
	HomeChannels []string `json:"home_channels"`
	Allowed      []string `json:"allowed_channels"`
	PeerBots     []string `json:"peer_bots"`
	// MentionOnly lists channels where the companion only wakes on a real
	// platform mention (never on name, reply or ambient chatter).
	MentionOnly []string `json:"mention_only_channels"`
	// RegisterCommands publishes the slash commands on startup. Turn it off when
	// sharing a bot application with another program: publishing replaces that
	// application\'s global commands.
	RegisterCommands bool `json:"register_commands"`
	// OnlyGuilds restricts her to these servers (IDs). Empty means every server
	// she is in. DMs are governed by owner_id, not by this list.
	OnlyGuilds []string `json:"only_guilds"`
	// HumanWebhooks lists webhook IDs whose messages count as people, not bots
	// (chat bridges, test harnesses). Any other webhook is treated as a bot.
	HumanWebhooks []string          `json:"human_webhooks"`
	Guilds        map[string]string `json:"guilds"`
}

type Memory struct {
	Path           string `json:"path"`
	RecallLimit    int    `json:"recall_limit"`
	MaintenanceHrs int    `json:"maintenance_hours"`
	AutoExtract    bool   `json:"auto_extract"`
}

type WebUI struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Token   string `json:"token,omitempty"` // required when Host is not loopback
	// HideMessages keeps what people said out of the live feed.
	HideMessages bool `json:"hide_messages"`
}

type Search struct {
	SearxngURL string `json:"searxng_url"`
}

type Tools struct {
	Disabled []string             `json:"disabled"`
	MCP      map[string]MCPServer `json:"mcp_servers"`
}

// MCPServer is an external Model Context Protocol server whose tools become
// Mak1zu tools. Only tools in Allow are exposed (empty Allow exposes none).
type MCPServer struct {
	Enabled bool              `json:"enabled"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	Allow   []string          `json:"allow"`
	Heavy   bool              `json:"heavy"`
}

type Config struct {
	Name     string   `json:"name"`
	Language string   `json:"language"` // default reply language hint, "auto" = English until the speaker writes another language
	Persona  Persona  `json:"persona"`
	LLM      LLM      `json:"llm"`
	Behavior Behavior `json:"behavior"`
	Discord  Discord  `json:"discord"`
	Memory   Memory   `json:"memory"`
	WebUI    WebUI    `json:"web_ui"`
	Search   Search   `json:"search"`
	Tools    Tools    `json:"tools"`
}

// Default returns a working config with no credentials.
func Default() Config {
	return Config{
		Name: "Mak1zu", Language: "auto",
		Persona: Persona{Active: "maki", Dir: "personas"},
		LLM: LLM{
			Temperature: 0.9,
			Routing:     Routing{Text: []string{"main"}, Vision: []string{"main"}},
			Providers: map[string]Provider{
				"main": {Enabled: true, BaseURL: "https://api.openai.com/v1", Model: "gpt-4.1-mini",
					Protocol: "chat", APIKeyEnv: "MAK1ZU_API_KEY", TimeoutSeconds: 60, CooldownSeconds: 30, Vision: true},
			},
		},
		Behavior: Behavior{
			Response: Response{MaxPerMinute: 10, AmbientCooldown: 8, RecentActiveSecs: 120, BurstSettleSecs: 2.5,
				PeerCooldownSecs: 60, MaxPeerExchanges: 4,
				Chances: Chances{Mentioned: 1, ReplyToHer: 1, NameInMessage: 0.95, Question: 0.75, ActiveConvo: 0.5, Interesting: 0.3, Peer: 0.6}},
			Timing: Timing{ThinkMin: 0.6, ThinkMax: 2.2, TypingPerCh: 0.045, TypingMax: 8},
			Turn:   Turn{MaxRounds: 3, MaxTokens: 240, HeavyTokens: 900, BudgetSeconds: 25, HistoryLimit: 30, MaxReplyChars: 1900, EmojiBudget: 2},
			Retry:  true,
		},
		Discord: Discord{TokenEnv: "MAK1ZU_DISCORD_TOKEN", RegisterCommands: true},
		Memory:  Memory{Path: "data/memory.db", RecallLimit: 6, MaintenanceHrs: 24, AutoExtract: true},
		WebUI:   WebUI{Enabled: true, Host: "127.0.0.1", Port: 8787},
	}
}

// Resolve returns the secret for a provider: env var first, then the file.
func (p Provider) Key() string {
	if p.APIKeyEnv != "" {
		if v := os.Getenv(p.APIKeyEnv); v != "" {
			return v
		}
	}
	return p.APIKey
}

func (d Discord) BotToken() string {
	if d.TokenEnv != "" {
		if v := os.Getenv(d.TokenEnv); v != "" {
			return v
		}
	}
	return d.Token
}

// Store is the live, concurrent view of the config file.
type Store struct {
	path string
	mu   sync.RWMutex
	cfg  Config
	subs []func(Config)
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, cfg: Default()}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cfg); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return s, s.cfg.Validate()
}

// New wraps an in-memory config (tests, `mak1zu chat` without a file).
func New(path string, c Config) *Store { return &Store{path: path, cfg: c} }

func (s *Store) Path() string { return s.path }

// Abs resolves a config-relative path against the config file's directory,
// so a whole install can be moved or run from anywhere.
func (s *Store) Abs(p string) string {
	if p == "" || p == ":memory:" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(s.path), p)
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// OnChange registers a live-apply callback.
func (s *Store) OnChange(f func(Config)) {
	s.mu.Lock()
	s.subs = append(s.subs, f)
	s.mu.Unlock()
}

func (c Config) Validate() error {
	if c.Persona.Active == "" {
		return fmt.Errorf("persona.active is empty")
	}
	for _, group := range [][]string{c.LLM.Routing.Text, c.LLM.Routing.Vision} {
		for _, n := range group {
			if _, ok := c.LLM.Providers[n]; !ok {
				return fmt.Errorf("routing references unknown provider %q", n)
			}
		}
	}
	for n, p := range c.LLM.Providers {
		if p.Enabled && p.BaseURL == "" {
			return fmt.Errorf("provider %q has no base_url", n)
		}
		if p.Protocol != "" && p.Protocol != "chat" && p.Protocol != "responses" {
			return fmt.Errorf("provider %q: unknown protocol %q", n, p.Protocol)
		}
	}
	if c.WebUI.Enabled && !isLoopback(c.WebUI.Host) && c.WebUI.Token == "" {
		return fmt.Errorf("web_ui.token is required when host %q is not loopback", c.WebUI.Host)
	}
	return nil
}

func isLoopback(h string) bool { return h == "127.0.0.1" || h == "::1" || h == "localhost" }

// Save writes the config atomically with 0600 permissions.
func (s *Store) Save(c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path, append(b, '\n'))
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cfg-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Patch applies {"a.b.c": value} edits to the file on disk (not to a cached
// copy), validates the result, saves it and notifies subscribers. A nil value
// deletes the key.
func (s *Store) Patch(edits map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw map[string]any
	b, err := os.ReadFile(s.path)
	if err == nil {
		if err := json.Unmarshal(b, &raw); err != nil {
			return err
		}
	} else {
		b, _ := json.Marshal(s.cfg)
		_ = json.Unmarshal(b, &raw)
	}
	for p, v := range edits {
		if sv, ok := v.(string); ok && sv == "***" {
			continue // a masked secret echoed back by the panel is "unchanged"
		}
		if err := setPath(raw, strings.Split(p, "."), v); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	nb, _ := json.Marshal(raw)
	next := Default()
	// Start from zero values for maps so deleted providers really disappear.
	next.LLM.Providers = nil
	if err := json.Unmarshal(nb, &next); err != nil {
		return err
	}
	if err := next.Validate(); err != nil {
		return err
	}
	out, _ := json.MarshalIndent(next, "", "  ")
	if err := writeAtomic(s.path, append(out, '\n')); err != nil {
		return err
	}
	s.cfg = next
	for _, f := range s.subs {
		go f(next)
	}
	return nil
}

func setPath(m map[string]any, parts []string, v any) error {
	for i, k := range parts {
		last := i == len(parts)-1
		if last {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
			return nil
		}
		next, ok := m[k].(map[string]any)
		if !ok {
			if _, exists := m[k]; exists {
				if _, isIdx := strconv.Atoi(parts[i+1]); isIdx == nil {
					return fmt.Errorf("arrays are replaced whole, not indexed")
				}
				return fmt.Errorf("%q is not an object", k)
			}
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	return nil
}

// Redacted returns the config as a map with every secret replaced by "***"
// (or "" when unset) so the panel can show which ones exist.
func (c Config) Redacted() map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	redact(m)
	return m
}

func redact(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			lk := strings.ToLower(k)
			if s, ok := val.(string); ok && (lk == "api_key" || lk == "token" || strings.HasSuffix(lk, "_secret")) && !strings.HasSuffix(lk, "_env") {
				if s != "" {
					x[k] = "***"
				}
				continue
			}
			redact(val)
		}
	case []any:
		for _, i := range x {
			redact(i)
		}
	}
}
