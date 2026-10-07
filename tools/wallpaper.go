package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// wallhavenURL is a var only so tests can use a local server.
var wallhavenURL = "https://wallhaven.cc/api/v1"

func validWallpaperHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && (u.Host == "w.wallhaven.cc" || u.Host == "wallhaven.cc")
}

// Wallpaper finds a real desktop wallpaper on wallhaven.cc (no key needed) and
// posts it as its own message after her text, so Discord shows the image. The
// model only chooses the search words: safe-for-work is fixed in code, and the
// link must come from wallhaven's own response, so she can neither post
// something unsafe through this tool nor make up a URL.
func Wallpaper() sdk.Tool {
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "wallpaper", Heavy: true,
		Description: "Find a real wallpaper image on wallhaven.cc and post it after your message. Use it ONLY when someone asks for a wallpaper or desktop background; for any other picture use image_search. Give short English search words (style, subject, mood), e.g. \"anime city night rain\". The image is attached for you; never paste or invent a link.",
		Schema:      Schema([]string{"query"}, map[string][2]string{"query": {"string", "a few English search words, no more than 8"}})},
		F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
			var a struct{ Query string }
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			q := strings.Join(strings.Fields(strings.Map(func(r rune) rune {
				if r < 32 || r == 127 {
					return ' '
				}
				return r
			}, a.Query)), " ")
			if q == "" {
				return "", errors.New("query required")
			}
			if len(q) > 80 {
				q = q[:80]
			}
			// purity=100 is safe-for-work only; categories=110 is general+anime
			u := wallhavenURL + "/search?categories=110&purity=100&atleast=1920x1080&sorting=relevance&q=" + url.QueryEscape(q)
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			req.Header.Set("User-Agent", "mak1zu (+https://github.com/snowarch/mak1zu)")
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if err != nil {
				return "", errors.New("wallpaper service unreachable")
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
			var v struct {
				Data []struct {
					Path       string `json:"path"`
					URL        string `json:"url"`
					Resolution string `json:"resolution"`
					Purity     string `json:"purity"`
				} `json:"data"`
			}
			if resp.StatusCode != 200 || json.Unmarshal(b, &v) != nil {
				return "", fmt.Errorf("wallpaper service answered %d", resp.StatusCode)
			}
			var ok []int
			for i, w := range v.Data {
				if w.Purity == "sfw" && validWallpaperHost(w.Path) && len(ok) < 8 {
					ok = append(ok, i)
				}
			}
			if len(ok) == 0 {
				return "", fmt.Errorf("no wallpaper found for %q: try other words", q)
			}
			w := v.Data[ok[rand.Intn(len(ok))]]
			if env.AttachLink != nil {
				env.AttachLink(w.Path)
			}
			return fmt.Sprintf("a %s wallpaper matching %q will follow your message (source %s). You cannot see it: talk about what you searched for and never invent colours or details of how it looks. Do not paste a link.", w.Resolution, q, w.URL), nil
		}}
}
