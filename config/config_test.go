package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func store(t *testing.T) *Store {
	p := filepath.Join(t.TempDir(), "c.json")
	s := New(p, Default())
	if err := s.Save(Default()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPatchCreatesAndDeletesNestedKeys(t *testing.T) {
	s := store(t)
	if err := s.Patch(map[string]any{"llm.providers.local": map[string]any{"enabled": true, "base_url": "http://127.0.0.1:11434/v1", "model": "x"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get().LLM.Providers["local"]; !ok {
		t.Fatal("provider not created")
	}
	if err := s.Patch(map[string]any{"llm.providers.local": nil}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get().LLM.Providers["local"]; ok {
		t.Fatal("provider not deleted")
	}
}

func TestPatchRefusesRoutingToDeletedProvider(t *testing.T) {
	s := store(t)
	if err := s.Patch(map[string]any{"llm.providers.main": nil}); err == nil {
		t.Fatal("deleting the routed provider must fail")
	}
	if _, ok := s.Get().LLM.Providers["main"]; !ok {
		t.Fatal("failed patch mutated state")
	}
}

func TestNonLoopbackPanelNeedsToken(t *testing.T) {
	s := store(t)
	if err := s.Patch(map[string]any{"web_ui.host": "0.0.0.0"}); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatal("open panel without a token accepted")
	}
}

func TestRedactedHidesSecretsButKeepsEnvNames(t *testing.T) {
	c := Default()
	p := c.LLM.Providers["main"]
	p.APIKey = "sk-xyz"
	c.LLM.Providers["main"] = p
	c.Discord.Token = "tok"
	r := c.Redacted()
	b := strings.Join([]string{asJSON(r)}, "")
	if strings.Contains(b, "sk-xyz") || strings.Contains(b, `"tok"`) {
		t.Fatal("secret in redacted output")
	}
	if !strings.Contains(b, "MAK1ZU_API_KEY") {
		t.Fatal("env var name should stay visible")
	}
}

func TestEnvBeatsFileForKeys(t *testing.T) {
	t.Setenv("X_KEY", "from-env")
	if k := (Provider{APIKeyEnv: "X_KEY", APIKey: "from-file"}).Key(); k != "from-env" {
		t.Fatal(k)
	}
}

func TestDiscordIDsStayStrings(t *testing.T) {
	s := store(t)
	if err := s.Patch(map[string]any{"discord.home_channels": []any{"1100000000000000001"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.Path())
	if !strings.Contains(string(b), `"1100000000000000001"`) {
		t.Fatal("snowflake not preserved as text")
	}
}

func asJSON(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}
