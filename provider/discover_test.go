package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
)

func TestNormalizeBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.together.xyz/v1":                   "https://api.together.xyz/v1",
		"https://api.together.xyz/v1/":                  "https://api.together.xyz/v1",
		"api.together.xyz/v1":                           "https://api.together.xyz/v1",
		"https://x.example/v1/chat/completions":         "https://x.example/v1",
		"https://x.example/v1/chat/completions?foo=bar": "https://x.example/v1",
		"https://x.example/openai/v1/responses":         "https://x.example/openai/v1",
		"  \"http://localhost:8000/v1/models\"  ":       "http://localhost:8000/v1",
		"localhost:11434":                               "http://localhost:11434",
		"localhost:11434/v1":                            "http://localhost:11434/v1",
		"192.168.1.20:8080/v1":                          "http://192.168.1.20:8080/v1",
		"box.local:1234":                                "http://box.local:1234",
		"":                                              "",
	} {
		if got := NormalizeBaseURL(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestKeyEnvFor(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.together.xyz/v1":              "TOGETHER_API_KEY",
		"https://api.fireworks.ai/inference/v1":    "FIREWORKS_API_KEY",
		"https://my-gateway.corp.example.co.uk/v1": "EXAMPLE_API_KEY",
		"https://ai-gw.acme.com/v1":                "ACME_API_KEY",
		"http://localhost:8000/v1":                 "MAK1ZU_API_KEY",
		"http://10.0.0.5:8000/v1":                  "MAK1ZU_API_KEY",
		"https://203.0.113.9/v1":                   "MAK1ZU_API_KEY",
		"":                                         "MAK1ZU_API_KEY",
	} {
		if got := KeyEnvFor(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestParseModelIDsShapes(t *testing.T) {
	cases := map[string][]string{
		`{"object":"list","data":[{"id":"a"},{"id":"b"}]}`:  {"a", "b"},
		`{"models":[{"name":"llama3:8b"},{"name":"qwen"}]}`: {"llama3:8b", "qwen"},
		`[{"id":"x"}]`:       {"x"},
		`{"data":[]}`:        {},
		`{"error":"nope"}`:   nil,
		`<html>hello</html>`: nil,
	}
	for in, want := range cases {
		got := parseModelIDs([]byte(in))
		if (got == nil) != (want == nil) || (got != nil && !reflect.DeepEqual(got, want)) {
			t.Errorf("%s -> %#v, want %#v", in, got, want)
		}
	}
}

func fakeModels(t *testing.T, prefix string, wantKey string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix+"/models" {
			w.WriteHeader(404)
			return
		}
		if wantKey != "" && r.Header.Get("Authorization") != "Bearer "+wantKey {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResolveEndpointAddsV1AndSaysSo(t *testing.T) {
	srv := fakeModels(t, "/v1", "")
	r := ResolveEndpoint(context.Background(), srv.URL, "")
	if r.BaseURL != srv.URL+"/v1" || len(r.Models) != 2 || !strings.Contains(r.Note, "/v1") || r.Err != nil {
		t.Fatalf("%+v", r)
	}
}

func TestResolveEndpointPastedRouteAndKey(t *testing.T) {
	srv := fakeModels(t, "/v1", "sekret")
	r := ResolveEndpoint(context.Background(), srv.URL+"/v1/chat/completions", "sekret")
	if r.BaseURL != srv.URL+"/v1" || len(r.Models) != 2 || r.Note != "" {
		t.Fatalf("%+v", r)
	}
	r = ResolveEndpoint(context.Background(), srv.URL+"/v1", "wrong")
	if r.Err == nil || r.BaseURL != srv.URL+"/v1" || len(r.Models) != 0 {
		t.Fatalf("a wrong key must not lose the address: %+v", r)
	}
	if strings.Contains(r.Err.Error(), "wrong") {
		t.Fatal("key leaked into the error")
	}
}

func TestResolveEndpointKeepsAddressWhenNothingAnswers(t *testing.T) {
	r := ResolveEndpoint(context.Background(), "http://127.0.0.1:1/v1", "")
	if r.BaseURL != "http://127.0.0.1:1/v1" || r.Err == nil {
		t.Fatalf("%+v", r)
	}
}

func TestListModelsStatusDistinguishesAuthFromMissing(t *testing.T) {
	srv := fakeModels(t, "/v1", "k")
	if _, st, err := ListModels(context.Background(), config.Provider{BaseURL: srv.URL + "/v1"}); err == nil || st != 401 {
		t.Fatalf("status %d err %v", st, err)
	}
	if _, st, err := ListModels(context.Background(), config.Provider{BaseURL: srv.URL}); err == nil || st != 404 {
		t.Fatalf("status %d err %v", st, err)
	}
}

func TestResolveEndpointFindsV1EvenWhenTheKeyIsWrong(t *testing.T) {
	srv := fakeModels(t, "/v1", "right")
	r := ResolveEndpoint(context.Background(), srv.URL, "wrong")
	if r.BaseURL != srv.URL+"/v1" || r.Status != 401 || r.Err == nil || r.Note == "" {
		t.Fatalf("a 401 on /v1 proves the path: %+v", r)
	}
}

func TestResolveEndpointAddsV1UnderAPrefix(t *testing.T) {
	srv := fakeModels(t, "/zen/go/v1", "")
	r := ResolveEndpoint(context.Background(), srv.URL+"/zen/go", "")
	if r.BaseURL != srv.URL+"/zen/go/v1" || len(r.Models) != 2 {
		t.Fatalf("%+v", r)
	}
}
