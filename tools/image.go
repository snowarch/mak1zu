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

// commonsURL is a var only so tests can use a local server.
var commonsURL = "https://commons.wikimedia.org/w/api.php"

func validImageHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && (u.Host == "upload.wikimedia.org" || u.Host == "thumb.wikimedia.org")
}

// ImageSearch finds a real picture of something on Wikimedia Commons (free
// images, no key) and posts it after her text. Like the wallpaper and GIF
// tools, the link comes from the service's own answer and is checked against
// its host, so she can neither invent a URL nor be steered to a foreign one.
func ImageSearch() sdk.Tool {
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "image_search", Heavy: true,
		Description: "Find a real photo or picture of something (an animal, a place, an object, a landmark, a person in public life) on Wikimedia Commons and post it after your message. Use it when someone asks for a picture or photo. Not for wallpapers (use wallpaper) and not for fan art or memes. Give short English search words, e.g. \"orange tabby cat\". The image is attached for you; never paste or invent a link.",
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
			qs := url.Values{}
			qs.Set("action", "query")
			qs.Set("generator", "search")
			qs.Set("gsrnamespace", "6") // files only
			qs.Set("gsrsearch", q+" filetype:bitmap")
			qs.Set("gsrlimit", "10")
			qs.Set("prop", "imageinfo")
			qs.Set("iiprop", "url|mime")
			qs.Set("iiurlwidth", "1280")
			qs.Set("format", "json")
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, commonsURL+"?"+qs.Encode(), nil)
			req.Header.Set("User-Agent", UserAgent)
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if err != nil {
				return "", errors.New("image service unreachable")
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			var v struct {
				Query struct {
					Pages map[string]struct {
						Index     int    `json:"index"`
						Title     string `json:"title"`
						ImageInfo []struct {
							ThumbURL string `json:"thumburl"`
							URL      string `json:"url"`
							Mime     string `json:"mime"`
						} `json:"imageinfo"`
					} `json:"pages"`
				} `json:"query"`
			}
			if resp.StatusCode != 200 || json.Unmarshal(b, &v) != nil {
				return "", fmt.Errorf("image service answered %d", resp.StatusCode)
			}
			type hit struct {
				index       int
				title, link string
			}
			var hits []hit
			for _, p := range v.Query.Pages {
				if len(p.ImageInfo) == 0 {
					continue
				}
				i := p.ImageInfo[0]
				link := i.ThumbURL
				if link == "" {
					link = i.URL
				}
				if u, err := url.Parse(link); err == nil {
					u.RawQuery = "" // the attribution tracking parameters only clutter the chat
					link = u.String()
				}
				if (i.Mime == "image/jpeg" || i.Mime == "image/png" || i.Mime == "image/webp") && validImageHost(link) {
					hits = append(hits, hit{p.Index, strings.TrimPrefix(p.Title, "File:"), link})
				}
			}
			if len(hits) == 0 {
				return "", fmt.Errorf("no picture found for %q: try other words", q)
			}
			// best matches first, then a little variety among the top few
			for i := range hits {
				for j := i + 1; j < len(hits); j++ {
					if hits[j].index < hits[i].index {
						hits[i], hits[j] = hits[j], hits[i]
					}
				}
			}
			h := hits[rand.Intn(min(len(hits), 3))]
			if env.AttachLink != nil {
				env.AttachLink(h.link)
			}
			return fmt.Sprintf("a picture will follow your message: %q from Wikimedia Commons. You cannot see it, you only know that title: react to what the title says and never invent details of how it looks. Do not paste a link.", h.title), nil
		}}
}
