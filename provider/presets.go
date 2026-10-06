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
	Vision   bool
	Headroom int // extra completion tokens for thinking models
	Note     string
}

// Presets are ordered for display: hosted first, local last.
var Presets = []Preset{
	{ID: "openai", Label: "OpenAI", BaseURL: "https://api.openai.com/v1", Protocol: "chat", Model: "gpt-4.1-mini", KeyEnv: "OPENAI_API_KEY", KeyURL: "https://platform.openai.com/api-keys", Vision: true},
	{ID: "anthropic", Label: "Anthropic (Claude)", BaseURL: "https://api.anthropic.com/v1", Protocol: "chat", Model: "claude-haiku-4-5-20251001", KeyEnv: "ANTHROPIC_API_KEY", KeyURL: "https://console.anthropic.com/settings/keys", Vision: true},
	{ID: "gemini", Label: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Protocol: "chat", Model: "gemini-2.5-flash", KeyEnv: "GEMINI_API_KEY", KeyURL: "https://aistudio.google.com/apikey", Vision: true},
	{ID: "openrouter", Label: "OpenRouter (any model)", BaseURL: "https://openrouter.ai/api/v1", Protocol: "chat", Model: "openai/gpt-4.1-mini", KeyEnv: "OPENROUTER_API_KEY", KeyURL: "https://openrouter.ai/keys", Vision: true},
	{ID: "groq", Label: "Groq", BaseURL: "https://api.groq.com/openai/v1", Protocol: "chat", Model: "llama-3.3-70b-versatile", KeyEnv: "GROQ_API_KEY", KeyURL: "https://console.groq.com/keys"},
	{ID: "deepseek", Label: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", Protocol: "chat", Model: "deepseek-chat", KeyEnv: "DEEPSEEK_API_KEY", KeyURL: "https://platform.deepseek.com/api_keys"},
	{ID: "mistral", Label: "Mistral", BaseURL: "https://api.mistral.ai/v1", Protocol: "chat", Model: "mistral-small-latest", KeyEnv: "MISTRAL_API_KEY", KeyURL: "https://console.mistral.ai/api-keys"},
	{ID: "opencode-go", Label: "opencode Go", BaseURL: "https://opencode.ai/zen/go/v1", Protocol: "chat", Model: "deepseek-v4-flash", Headroom: 1200, KeyEnv: "OPENCODE_API_KEY", KeyURL: "https://opencode.ai", Note: "a few models here only speak the responses protocol: switch protocol for those"},
	{ID: "ollama", Label: "Ollama (local, no key)", BaseURL: "http://localhost:11434/v1", Protocol: "chat", Model: "llama3.2", Note: "run `ollama pull llama3.2` first; any model you have pulled works"},
	{ID: "lmstudio", Label: "LM Studio (local, no key)", BaseURL: "http://localhost:1234/v1", Protocol: "chat", Model: "local-model", Note: "start the local server in LM Studio and use the model id it shows"},
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
