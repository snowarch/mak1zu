package provider

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/config"
)

// Router walks an ordered list of providers, skipping the ones whose circuit
// is open. When every candidate is open it tries them anyway: a lone provider
// must never be locked out by its own cooldown.
type Router struct {
	mu       sync.Mutex
	cfg      func() config.Config
	clients  map[string]Client // test seam; nil = build from config
	openTill map[string]time.Time
}

func NewRouter(get func() config.Config) *Router {
	return &Router{cfg: get, openTill: map[string]time.Time{}}
}

// SetClient injects a fake client (tests).
func (r *Router) SetClient(name string, c Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients == nil {
		r.clients = map[string]Client{}
	}
	r.clients[name] = c
}

func (r *Router) client(name string, c config.Config) (Client, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cl, ok := r.clients[name]; ok {
		return cl, true
	}
	p, ok := c.LLM.Providers[name]
	if !ok || !p.Enabled {
		return nil, false
	}
	return NewHTTP(name, p), true
}

func (r *Router) isOpen(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return time.Now().Before(r.openTill[name])
}

func (r *Router) trip(name string, c config.Config, err error) {
	switch KindOf(err) {
	case KindBadRequest, KindUnsupported, KindCanceled:
		return // our fault or the caller's, not an outage
	}
	cd := time.Duration(c.LLM.Providers[name].CooldownSeconds) * time.Second
	if cd <= 0 {
		cd = 30 * time.Second
	}
	r.mu.Lock()
	r.openTill[name] = time.Now().Add(cd)
	r.mu.Unlock()
}

// Status reports which providers are in cooldown (for the panel).
func (r *Router) Status() map[string]time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]time.Duration{}
	for n, t := range r.openTill {
		if d := time.Until(t); d > 0 {
			out[n] = d
		}
	}
	return out
}

// Complete routes by capability: vision turns use the vision list.
func (r *Router) Complete(ctx context.Context, req Request) (Response, error) {
	c := r.cfg()
	names := c.LLM.Routing.Text
	if req.HasImages() {
		names = c.LLM.Routing.Vision
	}
	if req.Temperature == nil && c.LLM.Temperature > 0 {
		t := c.LLM.Temperature
		req.Temperature = &t
	}
	var cands []string
	for _, n := range names {
		if !r.isOpen(n) {
			cands = append(cands, n)
		}
	}
	if len(cands) == 0 {
		cands = names
	}
	var last error = errors.New("no provider configured for this route")
	for _, n := range cands {
		cl, ok := r.client(n, c)
		if !ok {
			continue
		}
		resp, err := cl.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		last = err
		if KindOf(err) == KindCanceled {
			return Response{}, err
		}
		r.trip(n, c, err)
	}
	return Response{}, last
}
