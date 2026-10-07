package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/provider"
)

type fakeNet struct {
	locals   []provider.Local
	resolved map[string]provider.Resolved // by raw address
	probes   []provider.Diagnosis         // answered in order, the last one repeats
	probed   []config.Provider
	models   []string
}

func testWizard(input string, env map[string]string, n *fakeNet) (*wizard, *bytes.Buffer) {
	r := bufio.NewReader(strings.NewReader(input))
	var out bytes.Buffer
	return &wizard{
		in: r, out: &out,
		secret: func() string { l, _ := r.ReadString('\n'); return strings.TrimSpace(l) },
		getenv: func(k string) string { return env[k] },
		detect: func(context.Context) []provider.Local { return n.locals },
		resolve: func(_ context.Context, raw, key string) provider.Resolved {
			if r, ok := n.resolved[raw]; ok {
				return r
			}
			return provider.Resolved{BaseURL: provider.NormalizeBaseURL(raw), Err: errors.New("nothing answered")}
		},
		list: func(context.Context, config.Provider) ([]string, int, error) { return n.models, 200, nil },
		probe: func(_ context.Context, _ string, c config.Provider) provider.Diagnosis {
			n.probed = append(n.probed, c)
			i := len(n.probed) - 1
			if i >= len(n.probes) {
				i = len(n.probes) - 1
			}
			return n.probes[i]
		},
	}, &out
}

var ok = provider.Diagnosis{OK: true, Reply: "ok", Latency: 120 * time.Millisecond}

func TestCustomEndpointFromTheMenu(t *testing.T) {
	n := &fakeNet{
		resolved: map[string]provider.Resolved{"api.together.xyz": {BaseURL: "https://api.together.xyz/v1", Models: []string{"a", "b", "c"}, Note: "added /v1"}},
		probes:   []provider.Diagnosis{ok},
	}
	w, out := testWizard("custom\napi.together.xyz\nsk-secret\n2\nn\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := ch.P
	if p.BaseURL != "https://api.together.xyz/v1" || p.Model != "b" || p.APIKeyEnv != "TOGETHER_API_KEY" || p.Protocol != "chat" || p.Vision {
		t.Fatalf("%+v", p)
	}
	if ch.Key != "sk-secret" || !ch.Works {
		t.Fatalf("key %q works %v", ch.Key, ch.Works)
	}
	if len(n.probed) != 1 || n.probed[0].Key() != "sk-secret" {
		t.Fatalf("the live check must use the pasted key: %+v", n.probed)
	}
	if strings.Contains(out.String(), "sk-secret") {
		t.Fatal("the key was echoed")
	}
}

func TestPastingAURLAtTheMenuSkipsToCustom(t *testing.T) {
	n := &fakeNet{
		resolved: map[string]provider.Resolved{"http://localhost:8000/v1": {BaseURL: "http://localhost:8000/v1", Models: []string{"only-model"}}},
		probes:   []provider.Diagnosis{ok},
	}
	w, _ := testWizard("http://localhost:8000/v1\n\n\nn\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.P.Model != "only-model" || ch.P.APIKeyEnv != "" || ch.P.TimeoutSeconds != 120 {
		t.Fatalf("a local keyless server needs no key variable: %+v", ch.P)
	}
}

func TestCustomRowIsTheLastNumber(t *testing.T) {
	n := &fakeNet{probes: []provider.Diagnosis{ok}}
	w, out := testWizard("zzz\nzzz\nzzz\n", nil, n)
	_, err := w.choose(context.Background())
	if err == nil || !strings.Contains(err.Error(), "last entry") {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(out.String(), "Your own URL") {
		t.Fatalf("the menu must offer a custom endpoint:\n%s", out)
	}
	last := len(provider.Presets) + 1
	n.resolved = map[string]provider.Resolved{"localhost:1": {BaseURL: "http://localhost:1/v1", Models: []string{"m"}}}
	w, _ = testWizard(itoa(last)+"\nlocalhost:1\n\nn\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil || ch.ID != "custom" {
		t.Fatalf("%+v %v", ch, err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestStandardOpenAIEnvironmentVariablesPrefillCustom(t *testing.T) {
	n := &fakeNet{
		resolved: map[string]provider.Resolved{"https://gw.acme.com/v1": {BaseURL: "https://gw.acme.com/v1", Models: []string{"x", "y"}}},
		probes:   []provider.Diagnosis{ok},
	}
	env := map[string]string{"OPENAI_BASE_URL": "https://gw.acme.com/v1", "OPENAI_API_KEY": "sk-from-env"}
	w, _ := testWizard("custom\n\n\n1\nn\n", env, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.P.APIKeyEnv != "OPENAI_API_KEY" || ch.Key != "" || ch.P.Model != "x" {
		t.Fatalf("%+v key=%q", ch.P, ch.Key)
	}
}

func TestPresetUsesTheKeyAlreadyInTheEnvironment(t *testing.T) {
	n := &fakeNet{probes: []provider.Diagnosis{ok}}
	w, out := testWizard("openai\n\n", map[string]string{"OPENAI_API_KEY": "sk-env"}, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.Key != "" || ch.P.Model != "gpt-5.4-mini" || !ch.Works {
		t.Fatalf("%+v", ch)
	}
	if !strings.Contains(out.String(), "using OPENAI_API_KEY from your environment") || strings.Contains(out.String(), "sk-env") {
		t.Fatalf("\n%s", out)
	}
}

func TestDetectedLocalServerIsOfferedAndPicked(t *testing.T) {
	n := &fakeNet{
		locals: []provider.Local{
			{Name: "Ollama", BaseURL: "http://127.0.0.1:11434/v1", Models: []string{"llama3.2"}},
			{Name: "vLLM", BaseURL: "http://127.0.0.1:8000/v1", Models: []string{"qwen3", "gemma"}},
		},
		probes: []provider.Diagnosis{ok},
	}
	w, out := testWizard(itoa(len(provider.Presets)+1)+"\n2\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.P.BaseURL != "http://127.0.0.1:8000/v1" || ch.P.Model != "gemma" || ch.P.APIKeyEnv != "" {
		t.Fatalf("%+v", ch.P)
	}
	s := out.String()
	if !strings.Contains(s, "vLLM (local)") || !strings.Contains(s, "running now") {
		t.Fatalf("\n%s", s)
	}
	if strings.Count(s, "Ollama") != 1 {
		t.Fatalf("Ollama already has a preset row; it must be marked, not repeated:\n%s", s)
	}
}

func TestFailedCheckOffersAModelFix(t *testing.T) {
	bad := provider.Diagnosis{Problem: "model x not found", Fix: "pick one", Models: []string{"real-1", "real-2"}}
	n := &fakeNet{
		resolved: map[string]provider.Resolved{"localhost:1": {BaseURL: "http://localhost:1/v1", Models: []string{"x", "y"}}},
		probes:   []provider.Diagnosis{bad, ok},
	}
	w, out := testWizard("custom\nlocalhost:1\n\n1\nn\nm\nreal-2\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ch.Works || ch.P.Model != "real-2" || len(n.probed) != 2 {
		t.Fatalf("%+v probes=%d\n%s", ch, len(n.probed), out)
	}
}

func TestNoKeyMeansNoLiveCheckButAnHonestSentence(t *testing.T) {
	n := &fakeNet{probes: []provider.Diagnosis{ok}}
	w, out := testWizard("groq\n\n\n", nil, n)
	ch, err := w.choose(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.Works || len(n.probed) != 0 || !strings.Contains(out.String(), "no key yet") {
		t.Fatalf("%+v\n%s", ch, out)
	}
}

func TestClosedInputIsAnErrorNotALoop(t *testing.T) {
	n := &fakeNet{probes: []provider.Diagnosis{ok}}
	w, _ := testWizard("", nil, n)
	if _, err := w.choose(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestFlagsCustomEndpoint(t *testing.T) {
	n := &fakeNet{resolved: map[string]provider.Resolved{
		"http://localhost:8000": {BaseURL: "http://localhost:8000/v1", Models: []string{"solo"}, Note: "added /v1"},
		"https://gw.example/v1": {BaseURL: "https://gw.example/v1", Models: []string{"a", "b"}},
	}}
	w, _ := testWizard("", nil, n)
	ch, err := w.fromFlags(context.Background(), initOptions{BaseURL: "http://localhost:8000"})
	if err != nil || ch.P.Model != "solo" || ch.P.BaseURL != "http://localhost:8000/v1" || ch.P.APIKeyEnv != "" {
		t.Fatalf("%+v %v", ch.P, err)
	}
	_, err = w.fromFlags(context.Background(), initOptions{BaseURL: "https://gw.example/v1"})
	if err == nil || !strings.Contains(err.Error(), "--model") || !strings.Contains(err.Error(), "a, b") {
		t.Fatalf("a many-model endpoint must ask for --model and list ids: %v", err)
	}
	ch, err = w.fromFlags(context.Background(), initOptions{BaseURL: "https://gw.example/v1", Model: "b", Key: "k", Protocol: "responses", Vision: true, Headers: []string{"X-Team: red"}})
	if err != nil {
		t.Fatal(err)
	}
	if ch.P.APIKeyEnv != "GW_API_KEY" || ch.P.Protocol != "responses" || !ch.P.Vision || ch.P.Headers["X-Team"] != "red" || ch.Key != "k" {
		t.Fatalf("%+v", ch.P)
	}
	ch, err = w.fromFlags(context.Background(), initOptions{BaseURL: "https://gw.example/v1", Model: "b", KeyEnv: "MY_VAR"})
	if err != nil || ch.P.APIKeyEnv != "MY_VAR" || ch.Key != "" {
		t.Fatalf("%+v %v", ch.P, err)
	}
}

func TestFlagsErrorsSayWhatToDo(t *testing.T) {
	n := &fakeNet{}
	w, _ := testWizard("", nil, n)
	for _, o := range []initOptions{
		{Provider: "nope"},
		{Provider: "custom"},
		{Model: "x"},
		{Provider: "openai", Protocol: "grpc"},
		{Provider: "openai", Headers: []string{"nocolon"}},
	} {
		if _, err := w.fromFlags(context.Background(), o); err == nil {
			t.Errorf("%+v should fail", o)
		}
	}
	ch, err := w.fromFlags(context.Background(), initOptions{Provider: "ollama", Model: "qwen3"})
	if err != nil || ch.P.Model != "qwen3" || ch.P.BaseURL != "http://localhost:11434/v1" {
		t.Fatalf("a preset can have its model overridden: %+v %v", ch.P, err)
	}
}

func TestInitWritesTheKeyToEnvAndNeverToConfig(t *testing.T) {
	dir := t.TempDir()
	ch := choice{ID: "custom", Label: "your endpoint", Key: "sk-TOPSECRET", Works: true,
		P: provider.Custom("https://api.together.xyz/v1", "TOGETHER_API_KEY", false)}
	ch.P.Model = "m"
	if err := cmdInit(dir, ch); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(dir, ".makizu", "config.json"))
	env, _ := os.ReadFile(filepath.Join(dir, ".makizu", ".env"))
	if strings.Contains(string(cfg), "TOPSECRET") {
		t.Fatal("key in config.json")
	}
	if !strings.Contains(string(env), "TOGETHER_API_KEY=sk-TOPSECRET\n") || !strings.Contains(string(cfg), `"https://api.together.xyz/v1"`) {
		t.Fatalf("\n%s\n%s", env, cfg)
	}
	st, _ := os.Stat(filepath.Join(dir, ".makizu", ".env"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("%v", st.Mode())
	}
	if err := cmdInitArgs([]string{dir}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second init must refuse before asking anything: %v", err)
	}
}
