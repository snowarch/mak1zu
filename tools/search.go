package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// UserAgent is what the keyless search engines and image services see. main
// sets the version; it says who we are, which is also what gets us answered
// (DuckDuckGo serves this agent and turns a spoofed browser away).
var UserAgent = "mak1zu (+https://github.com/snowarch/mak1zu)"

// Search endpoints are vars only so tests can point them at a local server.
var (
	exaURL  = "https://mcp.exa.ai/mcp?tools=web_search_exa"
	ddgURL  = "https://html.duckduckgo.com/html/"
	bingURL = "https://www.bing.com/search"
)

const searchResults = 5

type engine struct {
	name string
	run  func(ctx context.Context, q string) (string, error)
}

// WebSearch is a search that works on a fresh install: nobody has to run a
// server or sign up for a key. Engines are tried in order until one returns
// results: the owner's SearXNG if configured, then Exa's public endpoint,
// DuckDuckGo and Bing. A failing engine only costs a fallback.
func WebSearch(searxURL func() string) sdk.Tool {
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "web_search", Heavy: true,
		Description: "Search the web for current information, news, facts, release dates, prices, how-tos, anything you do not already know. Returns titles, links and snippets; use read_url on a promising link for the full page.",
		Schema:      Schema([]string{"query"}, map[string][2]string{"query": {"string", "what to search for"}})},
		F: func(ctx context.Context, raw json.RawMessage, _ *sdk.CallEnv) (string, error) {
			a, err := args[struct{ Query string }](raw)
			if err != nil {
				return "", err
			}
			q := strings.Join(strings.Fields(a.Query), " ")
			if q == "" {
				return "", errors.New("query required")
			}
			if len(q) > 300 {
				q = q[:300]
			}
			var engines []engine
			if searxURL != nil {
				if base := searxURL(); base != "" {
					engines = append(engines, engine{"searxng", func(ctx context.Context, q string) (string, error) { return searx(ctx, base, q) }})
				}
			}
			engines = append(engines, engine{"exa", searchExa}, engine{"duckduckgo", searchDDG}, engine{"bing", searchBing})
			var failed []string
			for _, e := range engines {
				out, err := e.run(ctx, q)
				if err == nil && strings.TrimSpace(out) != "" && out != "no results" {
					return out, nil
				}
				if err != nil {
					failed = append(failed, e.name+": "+err.Error())
				} else {
					failed = append(failed, e.name+": no results")
				}
				if ctx.Err() != nil {
					break
				}
			}
			return "", fmt.Errorf("every search engine failed (%s)", strings.Join(failed, "; "))
		}}
}

// searchClient is a var so tests can reach a server on localhost; in
// production every search request goes through the SSRF-safe client.
var searchClient = func() *http.Client { return SafeClient(15 * time.Second) }

func doSearch(ctx context.Context, method, u, ctype string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if strings.Contains(u, "mcp") {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	resp, err := searchClient().Do(req)
	if err != nil {
		return nil, errors.New("unreachable")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return b, nil
}

// searchExa calls Exa's public MCP endpoint (a JSON-RPC tool call whose answer
// arrives as a server-sent event).
func searchExa(ctx context.Context, q string) (string, error) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "web_search_exa", "arguments": map[string]any{"query": q, "numResults": searchResults}}})
	b, err := doSearch(ctx, http.MethodPost, exaURL, "application/json", payload)
	if err != nil {
		return "", err
	}
	type rpc struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct{ Type, Text string }
		} `json:"result"`
	}
	var cands [][]byte
	for _, line := range bytes.Split(b, []byte("\n")) {
		if rest, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:")); ok {
			cands = append(cands, bytes.TrimSpace(rest))
		}
	}
	if len(cands) == 0 {
		cands = [][]byte{b} // plain JSON instead of an event stream
	}
	for _, c := range cands {
		var v rpc
		if json.Unmarshal(c, &v) != nil || v.Result.IsError {
			continue
		}
		for _, it := range v.Result.Content {
			if it.Type == "text" && len(strings.TrimSpace(it.Text)) > 40 {
				t := strings.TrimSpace(it.Text)
				if len(t) > 4500 {
					t = t[:4500] + "\n…"
				}
				return t, nil
			}
		}
	}
	return "", errors.New("empty answer")
}

var (
	ddgLinkRe    = regexp.MustCompile(`(?s)<a[^>]+class="result__a"[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	ddgSnippetRe = regexp.MustCompile(`(?s)<a[^>]+class="result__snippet"[^>]*>(.*?)</a>`)
)

func cleanText(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tagRe.ReplaceAllString(s, ""))), " ")
}

// ddgTarget unwraps DuckDuckGo's redirect links and drops its ads.
func ddgTarget(raw string) string {
	raw = html.UnescapeString(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Host, "duckduckgo.com") {
		if u.Path == "/y.js" {
			return "" // sponsored
		}
		if t := u.Query().Get("uddg"); t != "" {
			return t
		}
		return ""
	}
	return raw
}

func searchDDG(ctx context.Context, q string) (string, error) {
	b, err := doSearch(ctx, http.MethodGet, ddgURL+"?q="+url.QueryEscape(q), "", nil)
	if err != nil {
		return "", err
	}
	page := string(b)
	links := ddgLinkRe.FindAllStringSubmatch(page, -1)
	snips := ddgSnippetRe.FindAllStringSubmatch(page, -1)
	var out strings.Builder
	n := 0
	for i, l := range links {
		target := ddgTarget(l[1])
		if target == "" || !strings.HasPrefix(target, "http") {
			continue
		}
		n++
		snip := ""
		if i < len(snips) {
			snip = cleanText(snips[i][1])
		}
		fmt.Fprintf(&out, "%d. %s\n   %s\n   %s\n", n, cleanText(l[2]), target, snip)
		if n >= searchResults {
			break
		}
	}
	if n == 0 {
		return "", errors.New("no results")
	}
	return out.String(), nil
}

func searchBing(ctx context.Context, q string) (string, error) {
	b, err := doSearch(ctx, http.MethodGet, bingURL+"?format=rss&q="+url.QueryEscape(q), "", nil)
	if err != nil {
		return "", err
	}
	var v struct {
		Items []struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Desc  string `xml:"description"`
		} `xml:"channel>item"`
	}
	if err := xml.Unmarshal(b, &v); err != nil {
		return "", errors.New("unreadable answer")
	}
	var out strings.Builder
	n := 0
	for _, it := range v.Items {
		if !strings.HasPrefix(it.Link, "http") {
			continue
		}
		n++
		fmt.Fprintf(&out, "%d. %s\n   %s\n   %s\n", n, cleanText(it.Title), it.Link, cleanText(it.Desc))
		if n >= searchResults {
			break
		}
	}
	if n == 0 {
		return "", errors.New("no results")
	}
	return out.String(), nil
}
