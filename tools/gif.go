package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// nekosURL is a var only so tests can use a local server.
var nekosURL = "https://nekos.best/api/v2"

// gifCategories is the allowlist: the model picks a feeling, never a URL.
var gifCategories = map[string]bool{
	"angry": true, "baka": true, "bite": true, "blowkiss": true, "blush": true, "bonk": true, "bored": true,
	"carry": true, "clap": true, "confused": true, "cry": true, "cuddle": true, "dance": true, "facepalm": true,
	"handhold": true, "happy": true, "highfive": true, "hug": true, "kick": true, "kiss": true, "laugh": true,
	"lurk": true, "nod": true, "nom": true, "nope": true, "nya": true, "pat": true, "peck": true, "poke": true,
	"pout": true, "punch": true, "run": true, "salute": true, "shake": true, "shocked": true, "shrug": true,
	"sip": true, "slap": true, "sleep": true, "smile": true, "smug": true, "spin": true, "stare": true,
	"teehee": true, "think": true, "thumbsup": true, "tickle": true, "wave": true, "wink": true, "yawn": true, "yeet": true,
}

func validGIFHost(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && (u.Host == "nekos.best" || strings.HasSuffix(u.Host, ".nekos.best"))
}

// ReactionGIF sends an anime reaction GIF (nekos.best, no key needed) as its
// own message after her text. She chooses a category; the URL comes from the
// API and must be on nekos.best, so a model can never make her post an
// arbitrary link through this tool.
func ReactionGIF() sdk.Tool {
	cats := make([]string, 0, len(gifCategories))
	for c := range gifCategories {
		cats = append(cats, c)
	}
	return sdk.ToolFunc{S: sdk.ToolSpec{Name: "reaction_gif",
		Description: "An anime reaction GIF after your message, sparingly, never instead of answering. Categories: " + strings.Join(sortedKeys(gifCategories), ", ") + ".",
		Schema:      Schema([]string{"category"}, map[string][2]string{"category": {"string", "one of the listed categories"}})},
		F: func(ctx context.Context, raw json.RawMessage, env *sdk.CallEnv) (string, error) {
			var a struct{ Category string }
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			c := strings.ToLower(strings.TrimSpace(a.Category))
			if !gifCategories[c] {
				return "", fmt.Errorf("unknown category %q", a.Category)
			}
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, nekosURL+"/"+c, nil)
			resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
			if err != nil {
				return "", errors.New("gif service unreachable")
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			var v struct {
				Results []struct {
					URL       string `json:"url"`
					AnimeName string `json:"anime_name"`
				}
			}
			if resp.StatusCode != 200 || json.Unmarshal(b, &v) != nil || len(v.Results) == 0 || !validGIFHost(v.Results[0].URL) {
				return "", errors.New("no gif available")
			}
			if env.AttachLink != nil {
				env.AttachLink(v.Results[0].URL)
			}
			return fmt.Sprintf("a %s gif (from the anime %q) will follow your message; do not paste its link yourself", c, v.Results[0].AnimeName), nil
		}}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
