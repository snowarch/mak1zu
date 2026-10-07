package provider

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"sync"

	"github.com/snowarch/mak1zu/config"
)

// Preset is a known-good starting point for one backend. Models drift fast:
// a preset only has to be a sane default, and the panel or config.json can
// change it at any time.
type Preset struct {
	ID       string
	Label    string
	BaseURL  string
	Protocol string
	Model    string
	KeyEnv   string // env var the key is read from; "" for local servers
	KeyURL   string // where a human gets a key
	Cost     string // what using it costs, in a few words
	Vision   bool
	Headroom int // extra completion tokens for thinking models
	Note     string
}

// Presets are ordered for display: ways to start for free first, then paid
// hosts, local servers last. docs/PROVIDERS.md walks through each one.
var Presets = []Preset{
	{ID: "openrouter-free", Label: "OpenRouter (free models)", BaseURL: "https://openrouter.ai/api/v1", Protocol: "chat", Model: "openrouter/free", KeyEnv: "OPENROUTER_API_KEY", KeyURL: "https://openrouter.ai/keys", Cost: "free, rate-limited", Note: "picks any free model that fits the request; about 20 requests a minute plus a daily cap; free endpoints may train on your prompts (see your OpenRouter privacy settings)"},
	{ID: "cline", Label: "Cline", BaseURL: "https://api.cline.bot/api/v1", Protocol: "chat", Model: "google/gemma-4-31b-it:free", KeyEnv: "CLINE_API_KEY", KeyURL: "https://app.cline.bot", Vision: true, Cost: "free promos, then pay as you go", Note: "the free models rotate; GET /models lists every id, and the ones ending in :free cost nothing"},
	{ID: "opencode-go", Label: "opencode Go", BaseURL: "https://opencode.ai/zen/go/v1", Protocol: "chat", Model: "deepseek-v4-flash", Headroom: 1200, KeyEnv: "OPENCODE_API_KEY", KeyURL: "https://opencode.ai", Cost: "subscription", Note: "works from any client; a few models only speak the responses protocol: switch protocol for those"},
	{ID: "opencode-zen", Label: "opencode Zen (paid)", BaseURL: "https://opencode.ai/zen/v1", Protocol: "chat", Model: "deepseek-v4-flash", Headroom: 1200, KeyEnv: "OPENCODE_API_KEY", KeyURL: "https://opencode.ai", Cost: "pay as you go", Note: "needs credits; the models marked free there only work inside the OpenCode app and answer 403 here"},
	{ID: "openrouter", Label: "OpenRouter (any model)", BaseURL: "https://openrouter.ai/api/v1", Protocol: "chat", Model: "google/gemini-3.5-flash", KeyEnv: "OPENROUTER_API_KEY", KeyURL: "https://openrouter.ai/keys", Vision: true, Headroom: 1000, Cost: "pay as you go"},
	{ID: "gemini", Label: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Protocol: "chat", Model: "gemini-3.5-flash", KeyEnv: "GEMINI_API_KEY", KeyURL: "https://aistudio.google.com/apikey", Vision: true, Headroom: 1000, Cost: "free tier or pay as you go"},
	{ID: "groq", Label: "Groq", BaseURL: "https://api.groq.com/openai/v1", Protocol: "chat", Model: "llama-3.3-70b-versatile", KeyEnv: "GROQ_API_KEY", KeyURL: "https://console.groq.com/keys", Cost: "free tier or pay as you go"},
	{ID: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com", Protocol: "chat", Model: "deepseek-v4-flash", KeyEnv: "DEEPSEEK_API_KEY", KeyURL: "https://platform.deepseek.com/api_keys", Headroom: 1000, Cost: "pay as you go"},
	{ID: "mistral", Label: "Mistral", BaseURL: "https://api.mistral.ai/v1", Protocol: "chat", Model: "mistral-small-latest", KeyEnv: "MISTRAL_API_KEY", KeyURL: "https://console.mistral.ai/api-keys", Cost: "free tier or pay as you go"},
	{ID: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1", Protocol: "chat", Model: "gpt-5.4-mini", KeyEnv: "OPENAI_API_KEY", KeyURL: "https://platform.openai.com/api-keys", Vision: true, Headroom: 1000, Cost: "pay as you go"},
	{ID: "anthropic", Label: "Anthropic (Claude)", BaseURL: "https://api.anthropic.com/v1", Protocol: "chat", Model: "claude-haiku-4-5-20251001", KeyEnv: "ANTHROPIC_API_KEY", KeyURL: "https://console.anthropic.com/settings/keys", Vision: true, Cost: "pay as you go"},
	{ID: "ollama", Label: "Ollama (local)", BaseURL: "http://localhost:11434/v1", Protocol: "chat", Model: "llama3.2", Cost: "free, runs on your machine", Note: "run `ollama pull llama3.2` first; any model you have pulled works"},
	{ID: "lmstudio", Label: "LM Studio (local)", BaseURL: "http://localhost:1234/v1", Protocol: "chat", Model: "local-model", Cost: "free, runs on your machine", Note: "start the local server in LM Studio and use the model id it shows"},
}

// PresetByID finds a preset, case-insensitively.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if strings.EqualFold(p.ID, strings.TrimSpace(id)) {
			return p, true
		}
	}
	return Preset{}, false
}

// Provider turns a preset into a ready config block.
func (p Preset) Provider() config.Provider {
	return config.Provider{
		Enabled: true, BaseURL: p.BaseURL, Model: p.Model, Protocol: p.Protocol,
		APIKeyEnv: p.KeyEnv, TimeoutSeconds: 60, CooldownSeconds: 30, Vision: p.Vision, ReasoningRoom: p.Headroom,
	}
}

// UserAgent identifies this client. Bot protection in front of several
// providers rejects Go's default agent with an opaque 403, so every request
// says who it is. main sets the version.
var UserAgent = "mak1zu/dev (+https://github.com/snowarch/mak1zu)"

var (
	sessionOnce sync.Once
	sessionID   string
)

func processSession() string {
	sessionOnce.Do(func() {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		sessionID = "mak1zu-" + hex.EncodeToString(b)
	})
	return sessionID
}

// quirkHeaders are headers a known host insists on. They are defaults: anything
// in the provider's own headers wins.
func quirkHeaders(baseURL string) map[string]string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai"):
		return map[string]string{"x-opencode-session": processSession()}
	}
	return nil
}
