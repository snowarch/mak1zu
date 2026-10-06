package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// fetchTrusted is for URLs the *owner* configured (their SearXNG). Never pass
// user-supplied URLs here.
func fetchTrusted(ctx context.Context, u string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("search backend http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
