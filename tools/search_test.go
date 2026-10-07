package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/sdk"
)

const exaBody = "event: message\ndata: {\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"Title: Frieren season 2 date\\nURL: https://example.org/frieren\\nHighlights: Season 2 premiered on January 16.\"}]}}\n\n"

const ddgBody = `<div class="result"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.org%2Fa&amp;rut=x">Frieren &amp; friends</a>
<a class="result__snippet" href="x">The <b>second</b> season.</a></div>
<div class="result"><a rel="nofollow" class="result__a" href="https://duckduckgo.com/y.js?ad_provider=x">An ad</a><a class="result__snippet" href="x">buy now</a></div>
<div class="result"><a rel="nofollow" class="result__a" href="https://example.org/b">Plain link</a><a class="result__snippet" href="x">Second snippet</a></div>`

const bingBody = `<?xml version="1.0"?><rss><channel><title>q</title><item><title>Frieren - Wikipedia</title><link>https://en.wikipedia.org/wiki/Frieren</link><description>A fantasy manga &amp; anime.</description></item></channel></rss>`

// engines points every search endpoint at one local server and makes the
// client plain, because the real one (rightly) refuses localhost.
func engines(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	oe, od, ob, oc := exaURL, ddgURL, bingURL, searchClient
	exaURL, ddgURL, bingURL = s.URL+"/exa/mcp", s.URL+"/ddg/", s.URL+"/bing"
	searchClient = func() *http.Client { return s.Client() }
	t.Cleanup(func() { exaURL, ddgURL, bingURL, searchClient = oe, od, ob, oc })
}

func runSearch(t *testing.T, q string, searx func() string) (string, error) {
	t.Helper()
	return WebSearch(searx).Call(context.Background(), json.RawMessage(fmt.Sprintf(`{"query":%q}`, q)), &sdk.CallEnv{})
}

func TestSearchWorksWithNoConfigurationAtAll(t *testing.T) {
	engines(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/exa") {
			if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Accept"), "event-stream") {
				t.Errorf("exa wants a POST that accepts event-stream, got %s %q", r.Method, r.Header.Get("Accept"))
			}
			fmt.Fprint(w, exaBody)
			return
		}
		w.WriteHeader(500)
	})
	out, err := runSearch(t, "frieren season 2", nil)
	if err != nil || !strings.Contains(out, "premiered on January 16") {
		t.Fatal(out, err)
	}
}

func TestSearchFallsThroughDeadEnginesInOrder(t *testing.T) {
	var hit []string
	engines(t, func(w http.ResponseWriter, r *http.Request) {
		hit = append(hit, strings.Split(r.URL.Path, "/")[1])
		switch {
		case strings.HasPrefix(r.URL.Path, "/ddg"):
			fmt.Fprint(w, ddgBody)
		default:
			w.WriteHeader(503) // exa is down
		}
	})
	out, err := runSearch(t, "frieren", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hit, ",") != "exa,ddg" {
		t.Fatalf("engines tried: %v", hit)
	}
	if !strings.Contains(out, "https://example.org/a") || !strings.Contains(out, "Frieren & friends") || !strings.Contains(out, "The second season.") {
		t.Fatalf("redirect not unwrapped or text not cleaned:\n%s", out)
	}
	if strings.Contains(out, "An ad") || strings.Contains(out, "y.js") {
		t.Fatalf("sponsored result leaked:\n%s", out)
	}
	if !strings.Contains(out, "https://example.org/b") {
		t.Fatalf("plain results must survive:\n%s", out)
	}
}

func TestSearchFallsToBingLast(t *testing.T) {
	engines(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/bing") {
			fmt.Fprint(w, bingBody)
			return
		}
		fmt.Fprint(w, "<html>captcha, no results</html>") // 200 but nothing usable
	})
	out, err := runSearch(t, "frieren", nil)
	if err != nil || !strings.Contains(out, "en.wikipedia.org/wiki/Frieren") || !strings.Contains(out, "anime.") {
		t.Fatal(out, err)
	}
}

func TestSearchOwnersSearxngGoesFirst(t *testing.T) {
	searx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results":[{"title":"mine","url":"https://mine.example","content":"from my instance"}]}`)
	}))
	defer searx.Close()
	engines(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("fell past the owner's instance to %s", r.URL.Path)
	})
	out, err := runSearch(t, "anything", func() string { return searx.URL })
	if err != nil || !strings.Contains(out, "from my instance") {
		t.Fatal(out, err)
	}
}

func TestSearchSaysSoWhenEverythingFails(t *testing.T) {
	engines(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	_, err := runSearch(t, "frieren", nil)
	if err == nil || !strings.Contains(err.Error(), "exa") || !strings.Contains(err.Error(), "bing") {
		t.Fatalf("want an error naming the engines, got %v", err)
	}
	if _, err := runSearch(t, "   ", nil); err == nil {
		t.Fatal("empty query accepted")
	}
}

func TestSearchRegisteredEvenWithoutSearxng(t *testing.T) {
	for _, tool := range Builtins(Deps{}) {
		if tool.Spec().Name == "web_search" {
			return
		}
	}
	t.Fatal("web_search is missing from a default install: she cannot look anything up")
}
