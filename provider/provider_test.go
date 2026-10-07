package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
)

func srv(t *testing.T, h func(path string, body map[string]any) (int, string)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(b, &m)
		code, out := h(r.URL.Path, m)
		w.WriteHeader(code)
		io.WriteString(w, out)
	}))
}

func TestChatProtocolTextAndToolCalls(t *testing.T) {
	s := srv(t, func(p string, b map[string]any) (int, string) {
		if p != "/chat/completions" || b["max_tokens"].(float64) != 300 {
			t.Errorf("path %s body %v", p, b)
		}
		return 200, `{"choices":[{"message":{"content":"<think>hm</think>hello","tool_calls":[{"id":"c","function":{"name":"now","arguments":"{}"}}]}}]}`
	})
	defer s.Close()
	h := NewHTTP("a", config.Provider{Enabled: true, BaseURL: s.URL, Model: "m", ReasoningRoom: 100})
	r, err := h.Complete(context.Background(), Request{MaxTokens: 200, Messages: []Message{{Role: User, Content: "hi"}}})
	if err != nil || r.Text != "hello" || len(r.ToolCalls) != 1 || r.ToolCalls[0].Name != "now" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestResponsesProtocolForModelsThatRejectChat(t *testing.T) {
	s := srv(t, func(p string, b map[string]any) (int, string) {
		if p != "/responses" || b["instructions"] != "sys" {
			t.Errorf("path %s body %v", p, b)
		}
		return 200, `{"output":[{"type":"reasoning"},{"type":"message","content":[{"type":"output_text","text":"yo"}]},{"type":"function_call","call_id":"k","name":"now","arguments":"{}"}]}`
	})
	defer s.Close()
	h := NewHTTP("a", config.Provider{Enabled: true, BaseURL: s.URL, Model: "m", Protocol: "responses"})
	r, err := h.Complete(context.Background(), Request{System: "sys", Messages: []Message{{Role: User, Content: "hi"}}})
	if err != nil || r.Text != "yo" || r.ToolCalls[0].ID != "k" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestEmptyCompletionIsClassified(t *testing.T) {
	s := srv(t, func(string, map[string]any) (int, string) {
		return 200, `{"choices":[{"message":{"content":"<think>only thinking</think>"}}]}`
	})
	defer s.Close()
	_, err := NewHTTP("a", config.Provider{Enabled: true, BaseURL: s.URL}).Complete(context.Background(), Request{})
	if KindOf(err) != KindEmpty {
		t.Fatal(err)
	}
}

func TestStatusClassificationAndKeyNeverLeaks(t *testing.T) {
	for code, want := range map[int]Kind{401: KindAuth, 429: KindRateLimit, 400: KindBadRequest, 502: KindServer} {
		s := srv(t, func(string, map[string]any) (int, string) { return code, `bad key sk-SECRET123` })
		_, err := NewHTTP("a", config.Provider{Enabled: true, BaseURL: s.URL, APIKey: "sk-SECRET123"}).Complete(context.Background(), Request{})
		s.Close()
		if KindOf(err) != want {
			t.Errorf("%d -> %v want %v", code, KindOf(err), want)
		}
		if strings.Contains(err.Error(), "SECRET123") {
			t.Errorf("key leaked in error: %v", err)
		}
	}
}

func TestVisionRequiresCapability(t *testing.T) {
	h := NewHTTP("a", config.Provider{Enabled: true, BaseURL: "http://unused", Vision: false})
	_, err := h.Complete(context.Background(), Request{Messages: []Message{{Role: User, Images: []string{"data:image/png;base64,AA"}}}})
	if KindOf(err) != KindUnsupported {
		t.Fatal(err)
	}
}

func TestRouterFallsBackAndCircuitSkipsDeadProvider(t *testing.T) {
	var aCalls, bCalls int
	a := srv(t, func(string, map[string]any) (int, string) { aCalls++; return 502, "down" })
	b := srv(t, func(string, map[string]any) (int, string) {
		bCalls++
		return 200, `{"choices":[{"message":{"content":"from b"}}]}`
	})
	defer a.Close()
	defer b.Close()
	cfg := config.Default()
	cfg.LLM.Providers = map[string]config.Provider{
		"a": {Enabled: true, BaseURL: a.URL, CooldownSeconds: 60},
		"b": {Enabled: true, BaseURL: b.URL},
	}
	cfg.LLM.Routing = config.Routing{Text: []string{"a", "b"}}
	r := NewRouter(func() config.Config { return cfg })
	for i := 0; i < 3; i++ {
		resp, err := r.Complete(context.Background(), Request{})
		if err != nil || resp.Text != "from b" {
			t.Fatal(resp, err)
		}
	}
	if aCalls != 1 || bCalls != 3 {
		t.Fatalf("circuit did not skip dead provider: a=%d b=%d", aCalls, bCalls)
	}
}

func TestRouterTriesLoneOpenProviderAnyway(t *testing.T) {
	n := 0
	a := srv(t, func(string, map[string]any) (int, string) {
		n++
		if n == 1 {
			return 502, "x"
		}
		return 200, `{"choices":[{"message":{"content":"back"}}]}`
	})
	defer a.Close()
	cfg := config.Default()
	cfg.LLM.Providers = map[string]config.Provider{"a": {Enabled: true, BaseURL: a.URL, CooldownSeconds: 300}}
	cfg.LLM.Routing = config.Routing{Text: []string{"a"}}
	r := NewRouter(func() config.Config { return cfg })
	r.Complete(context.Background(), Request{})
	if resp, err := r.Complete(context.Background(), Request{}); err != nil || resp.Text != "back" {
		t.Fatalf("a lone provider must not be locked out by its own cooldown: %v %v", resp, err)
	}
}

func TestLearnsCompletionTokensAndTemperatureFromARefusal(t *testing.T) {
	var calls int
	s := srv(t, func(_ string, b map[string]any) (int, string) {
		calls++
		_, oldCap := b["max_tokens"]
		_, hasTemp := b["temperature"]
		switch {
		case oldCap:
			return 400, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`
		case hasTemp:
			return 400, `{"error":{"message":"Unsupported value: 'temperature' does not support 0.9 with this model. Only the default (1) value is supported."}}`
		}
		if b["max_completion_tokens"] == nil {
			t.Errorf("lost the token cap: %v", b)
		}
		return 200, `{"choices":[{"message":{"content":"fine"}}]}`
	})
	defer s.Close()
	temp := 0.9
	cfg := config.Provider{Enabled: true, BaseURL: s.URL, Model: "learns-1"}
	req := Request{Temperature: &temp, Messages: []Message{{Role: User, Content: "hi"}}}
	r, err := NewHTTP("a", cfg).Complete(context.Background(), req)
	if err != nil || r.Text != "fine" {
		t.Fatalf("%+v %v", r, err)
	}
	first := calls
	// a brand new client for the same endpoint and model starts out knowing
	if _, err := NewHTTP("a", cfg).Complete(context.Background(), req); err != nil || calls != first+1 {
		t.Fatalf("second client did not start informed: calls %d -> %d, %v", first, calls, err)
	}
}

func TestOtherBadRequestsAreNotRetried(t *testing.T) {
	var calls int
	s := srv(t, func(string, map[string]any) (int, string) {
		calls++
		return 400, `{"error":{"message":"messages: must not be empty"}}`
	})
	defer s.Close()
	_, err := NewHTTP("a", config.Provider{Enabled: true, BaseURL: s.URL, Model: "plain-1"}).Complete(context.Background(), Request{})
	if KindOf(err) != KindBadRequest || calls != 1 {
		t.Fatalf("calls %d err %v", calls, err)
	}
}

func TestCleanTextDropsThoughtsEvenWhenCutOff(t *testing.T) {
	for in, want := range map[string]string{
		"<think>hm</think>hello":              "hello",
		"hi <think>still thinking about the":  "hi",
		"<think>never finished":               "",
		"thought</think>the answer":           "the answer",
		"plain text with <thinking> in prose": "plain text with <thinking> in prose",
	} {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}
