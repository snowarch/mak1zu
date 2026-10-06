package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/sdk"
)

// anilistURL is a var only so tests can point it at a local server.
var anilistURL = "https://graphql.anilist.co"

func anilist(ctx context.Context, query string, vars map[string]any) (json.RawMessage, error) {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, anilistURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// Fixed, trusted host: a plain client is fine (no user-supplied URL here).
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anilist http %d", resp.StatusCode)
	}
	var v struct {
		Data   json.RawMessage
		Errors []struct{ Message string }
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	if len(v.Errors) > 0 {
		return nil, fmt.Errorf("anilist: %s", v.Errors[0].Message)
	}
	return v.Data, nil
}

// Anime returns the weeb toolbox: search and airing schedule from AniList.
func Anime() []sdk.Tool {
	return []sdk.Tool{
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "anime_search", Heavy: true, Description: "Look up an anime or manga on AniList: score, episodes, status, genres, synopsis.",
			Schema: Schema([]string{"query"}, map[string][2]string{"query": {"string", "title"}, "kind": {"string", "ANIME (default) or MANGA"}})},
			F: func(ctx context.Context, raw json.RawMessage, _ *sdk.CallEnv) (string, error) {
				var a struct{ Query, Kind string }
				if err := json.Unmarshal(raw, &a); err != nil || strings.TrimSpace(a.Query) == "" {
					return "", fmt.Errorf("query required")
				}
				kind := "ANIME"
				if strings.EqualFold(a.Kind, "manga") {
					kind = "MANGA"
				}
				data, err := anilist(ctx, `query($q:String,$t:MediaType){Media(search:$q,type:$t){title{romaji english} averageScore episodes chapters status season seasonYear genres description(asHtml:false) siteUrl}}`,
					map[string]any{"q": a.Query, "t": kind})
				if err != nil {
					return "", err
				}
				var v struct {
					Media struct {
						Title                struct{ Romaji, English string }
						AverageScore         int
						Episodes, Chapters   int
						Status, Season       string
						SeasonYear           int
						Genres               []string
						Description, SiteURL string
					}
				}
				if err := json.Unmarshal(data, &v); err != nil || v.Media.Title.Romaji == "" {
					return "no match", nil
				}
				m := v.Media
				name := m.Title.English
				if name == "" {
					name = m.Title.Romaji
				}
				desc := HTMLToText(m.Description)
				if len(desc) > 500 {
					desc = desc[:500] + "…"
				}
				return fmt.Sprintf("%s (%s) | score %d/100 | %s | eps %d ch %d | %s %d | %s\n%s\n%s", name, m.Title.Romaji, m.AverageScore, m.Status, m.Episodes, m.Chapters, m.Season, m.SeasonYear, strings.Join(m.Genres, ", "), desc, m.SiteURL), nil
			}},
		sdk.ToolFunc{S: sdk.ToolSpec{Name: "anime_airing", Heavy: true, Description: "What anime episodes air today, tomorrow, or in the next week (AniList schedule).",
			Schema: Schema(nil, map[string][2]string{"timeframe": {"string", "today, tomorrow or week"}})},
			F: func(ctx context.Context, raw json.RawMessage, _ *sdk.CallEnv) (string, error) {
				var a struct{ Timeframe string }
				_ = json.Unmarshal(raw, &a)
				now := time.Now()
				start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
				from, to := start, start.Add(24*time.Hour)
				switch strings.ToLower(a.Timeframe) {
				case "tomorrow":
					from, to = to, to.Add(24*time.Hour)
				case "week":
					to = start.Add(7 * 24 * time.Hour)
				}
				data, err := anilist(ctx, `query($a:Int,$b:Int){Page(perPage:25){airingSchedules(airingAt_greater:$a,airingAt_lesser:$b,sort:TIME){airingAt episode media{title{romaji english} averageScore}}}}`,
					map[string]any{"a": from.Unix(), "b": to.Unix()})
				if err != nil {
					return "", err
				}
				var v struct {
					Page struct {
						AiringSchedules []struct {
							AiringAt int64
							Episode  int
							Media    struct {
								Title        struct{ Romaji, English string }
								AverageScore int
							}
						}
					}
				}
				if err := json.Unmarshal(data, &v); err != nil || len(v.Page.AiringSchedules) == 0 {
					return "nothing scheduled in that window", nil
				}
				var b strings.Builder
				for _, s := range v.Page.AiringSchedules {
					n := s.Media.Title.English
					if n == "" {
						n = s.Media.Title.Romaji
					}
					fmt.Fprintf(&b, "%s ep %d at %s (score %d)\n", n, s.Episode, time.Unix(s.AiringAt, 0).Local().Format("Mon 15:04"), s.Media.AverageScore)
				}
				return b.String(), nil
			}},
	}
}
