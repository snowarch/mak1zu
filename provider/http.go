package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/config"
)

// HTTP is one configured OpenAI-compatible endpoint.
type HTTP struct {
	Name string
	Cfg  config.Provider
	HC   *http.Client
}

func NewHTTP(name string, c config.Provider) *HTTP {
	to := time.Duration(c.TimeoutSeconds) * time.Second
	if to <= 0 {
		to = 60 * time.Second
	}
	return &HTTP{Name: name, Cfg: c, HC: &http.Client{Timeout: to}}
}

var thinkRe = regexp.MustCompile(`(?s)<think(?:ing)?>.*?</think(?:ing)?>`)

// CleanText removes provider reasoning blocks that some models inline.
func CleanText(s string) string {
	s = thinkRe.ReplaceAllString(s, "")
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = s[i+len("</think>"):]
	}
	// a reply cut off mid-thought has an opening tag and nothing after it
	if i := strings.Index(s, "<think>"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func (h *HTTP) Complete(ctx context.Context, r Request) (Response, error) {
	start := time.Now()
	if r.HasImages() && !h.Cfg.Vision {
		return Response{}, &Error{Kind: KindUnsupported, Provider: h.Name, Msg: "provider has no vision"}
	}
	path := "/chat/completions"
	if h.Cfg.Protocol == "responses" {
		path = "/responses"
	}
	var data []byte
	var status int
	// retry only while the provider keeps telling us a new parameter it dislikes
	for attempt := 0; ; attempt++ {
		var err *Error
		status, data, err = h.post(ctx, path, h.body(path, r))
		if err != nil {
			return Response{}, err
		}
		if status < 300 {
			break
		}
		if attempt < 2 && status == http.StatusBadRequest && h.learn(data) {
			continue
		}
		return Response{}, &Error{Kind: classify(status, data), Provider: h.Name, Status: status, Msg: scrub(snippet(data), h.Cfg.Key())}
	}
	var out Response
	var err error
	if h.Cfg.Protocol == "responses" {
		out, err = parseResponses(data)
	} else {
		out, err = parseChat(data)
	}
	if err != nil {
		return Response{}, &Error{Kind: KindServer, Provider: h.Name, Status: status, Msg: "malformed response: " + err.Error()}
	}
	out.Text = CleanText(out.Text)
	if out.Text == "" && len(out.ToolCalls) == 0 {
		return Response{}, &Error{Kind: KindEmpty, Provider: h.Name, Status: status, Msg: "empty completion (raise reasoning_headroom for thinking models)"}
	}
	out.Provider, out.Model, out.Latency = h.Name, h.Cfg.Model, time.Since(start)
	return out, nil
}

// body builds the request for the protocol, adapted to what this endpoint and
// model have already refused, then the user's extra_body on top.
func (h *HTTP) body(path string, r Request) map[string]any {
	var b map[string]any
	if path == "/responses" {
		b = h.responsesBody(r)
	} else {
		b = h.chatBody(r)
	}
	q := h.learned()
	if q&quirkCompletionTokens != 0 {
		if n, ok := b["max_tokens"]; ok {
			delete(b, "max_tokens")
			b["max_completion_tokens"] = n
		}
	}
	if q&quirkNoTemperature != 0 {
		delete(b, "temperature")
	}
	for k, v := range h.Cfg.ExtraBody {
		b[k] = v
	}
	return b
}

// post sends one request. A returned *Error is a transport failure; HTTP
// statuses come back as (status, body).
func (h *HTTP) post(ctx context.Context, path string, body map[string]any) (int, []byte, *Error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.Cfg.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, &Error{Kind: KindBadRequest, Provider: h.Name, Msg: err.Error()}
	}
	h.decorate(req)
	resp, err := h.HC.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return 0, nil, &Error{Kind: KindCanceled, Provider: h.Name, Msg: "canceled"}
		}
		k := KindServer
		var ne interface{ Timeout() bool }
		if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
			k = KindTimeout
		}
		return 0, nil, &Error{Kind: k, Provider: h.Name, Msg: scrub(err.Error(), h.Cfg.Key())}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, data, nil
}

// Parameter quirks a provider taught us by refusing a request. OpenAI's newer
// models reject max_tokens and any temperature but their default; there is no
// list to ship that would stay true, so the client reads the refusal once and
// remembers it for the life of the process.
const (
	quirkCompletionTokens = 1 << iota // wants max_completion_tokens, not max_tokens
	quirkNoTemperature                // only accepts its default temperature
)

var learnedQuirks sync.Map // "base_url|model" -> int

func (h *HTTP) quirkKey() string { return h.Cfg.BaseURL + "|" + h.Cfg.Model }

func (h *HTTP) learned() int {
	v, _ := learnedQuirks.Load(h.quirkKey())
	q, _ := v.(int)
	return q
}

// learn reads a 400 body and reports whether it taught us something new.
func (h *HTTP) learn(body []byte) bool {
	low := strings.ToLower(string(body))
	have, add := h.learned(), 0
	if strings.Contains(low, "max_completion_tokens") {
		add |= quirkCompletionTokens
	}
	if strings.Contains(low, "temperature") {
		add |= quirkNoTemperature
	}
	if add&^have == 0 {
		return false
	}
	learnedQuirks.Store(h.quirkKey(), have|add)
	return true
}

// decorate sets the headers every request carries: content type, key, an honest
// user agent, the host's own quirks, then the user's overrides.
func (h *HTTP) decorate(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if k := h.Cfg.Key(); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	for k, v := range quirkHeaders(h.Cfg.BaseURL) {
		req.Header.Set(k, v)
	}
	for k, v := range h.Cfg.Headers {
		req.Header.Set(k, v)
	}
}

func classify(status int, body []byte) Kind {
	switch {
	case status == 401 || status == 403:
		return KindAuth
	case status == 429:
		return KindRateLimit
	case status == 404 && strings.Contains(strings.ToLower(string(body)), "model"):
		return KindBadRequest
	case status == 400 || status == 404 || status == 410 || status == 422:
		return KindBadRequest
	case status == 408 || status == 504:
		return KindTimeout
	}
	return KindServer
}

func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// scrub removes the API key from anything that might reach a log.
func scrub(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "***")
	}
	return s
}

func (h *HTTP) budget(r Request) int {
	n := r.MaxTokens
	if n <= 0 {
		n = 400
	}
	return n + h.Cfg.ReasoningRoom
}

func (h *HTTP) temp(r Request) *float64 {
	if r.Temperature != nil {
		return r.Temperature
	}
	return h.Cfg.Temperature
}

// ---- chat protocol ----

func (h *HTTP) chatBody(r Request) map[string]any {
	var msgs []map[string]any
	if r.System != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": r.System})
	}
	for _, m := range r.Messages {
		e := map[string]any{"role": string(m.Role)}
		switch {
		case m.Role == ToolRole:
			e["tool_call_id"], e["content"] = m.ToolCallID, m.Content
		case len(m.Images) > 0:
			parts := []map[string]any{{"type": "text", "text": m.Content}}
			for _, u := range m.Images {
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": u}})
			}
			e["content"] = parts
		default:
			e["content"] = m.Content
		}
		if m.Name != "" && m.Role == User {
			e["name"] = sanitizeName(m.Name)
		}
		if len(m.ToolCalls) > 0 {
			var tcs []map[string]any
			for _, c := range m.ToolCalls {
				tcs = append(tcs, map[string]any{"id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Args}})
			}
			e["tool_calls"] = tcs
		}
		msgs = append(msgs, e)
	}
	b := map[string]any{"model": h.Cfg.Model, "messages": msgs, "max_tokens": h.budget(r)}
	if t := h.temp(r); t != nil {
		b["temperature"] = *t
	}
	if h.Cfg.ReasoningEffort != "" {
		b["reasoning_effort"] = h.Cfg.ReasoningEffort
	}
	if len(r.Tools) > 0 {
		var ts []map[string]any
		for _, t := range r.Tools {
			ts = append(ts, map[string]any{"type": "function", "function": map[string]any{
				"name": t.Name, "description": t.Description, "parameters": json.RawMessage(t.Schema)}})
		}
		b["tools"] = ts
	}
	return b
}

var nameRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// sanitizeName fits the strict `name` field some chat APIs enforce.
func sanitizeName(n string) string {
	n = nameRe.ReplaceAllString(n, "_")
	if len(n) > 64 {
		n = n[:64]
	}
	return n
}

func parseChat(data []byte) (Response, error) {
	var v struct {
		Choices []struct {
			Message struct {
				Content   json.RawMessage `json:"content"`
				ToolCalls []struct {
					ID       string                           `json:"id"`
					Function struct{ Name, Arguments string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return Response{}, err
	}
	if len(v.Choices) == 0 {
		return Response{}, errors.New("no choices")
	}
	m := v.Choices[0].Message
	out := Response{Text: textOf(m.Content)}
	for _, c := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{ID: c.ID, Name: c.Function.Name, Args: c.Function.Arguments})
	}
	return out, nil
}

// textOf accepts a string or an array of {type,text} parts.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "" || p.Type == "text" || p.Type == "output_text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return ""
}

// ---- responses protocol ----

func (h *HTTP) responsesBody(r Request) map[string]any {
	var in []map[string]any
	for _, m := range r.Messages {
		switch {
		case m.Role == ToolRole:
			in = append(in, map[string]any{"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content})
		case m.Role == Assistant && len(m.ToolCalls) > 0:
			if m.Content != "" {
				in = append(in, map[string]any{"role": "assistant", "content": m.Content})
			}
			for _, c := range m.ToolCalls {
				in = append(in, map[string]any{"type": "function_call", "call_id": c.ID, "name": c.Name, "arguments": c.Args})
			}
		case len(m.Images) > 0:
			parts := []map[string]any{{"type": "input_text", "text": m.Content}}
			for _, u := range m.Images {
				parts = append(parts, map[string]any{"type": "input_image", "image_url": u})
			}
			in = append(in, map[string]any{"role": string(m.Role), "content": parts})
		default:
			in = append(in, map[string]any{"role": string(m.Role), "content": m.Content})
		}
	}
	b := map[string]any{"model": h.Cfg.Model, "input": in, "max_output_tokens": h.budget(r)}
	if r.System != "" {
		b["instructions"] = r.System
	}
	if t := h.temp(r); t != nil {
		b["temperature"] = *t
	}
	if h.Cfg.ReasoningEffort != "" {
		b["reasoning"] = map[string]any{"effort": h.Cfg.ReasoningEffort}
	}
	if len(r.Tools) > 0 {
		var ts []map[string]any
		for _, t := range r.Tools {
			ts = append(ts, map[string]any{"type": "function", "name": t.Name, "description": t.Description, "parameters": json.RawMessage(t.Schema)})
		}
		b["tools"] = ts
	}
	return b
}

func parseResponses(data []byte) (Response, error) {
	var v struct {
		OutputText string `json:"output_text"`
		Output     []struct {
			Type      string                        `json:"type"`
			CallID    string                        `json:"call_id"`
			Name      string                        `json:"name"`
			Arguments string                        `json:"arguments"`
			Content   []struct{ Type, Text string } `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return Response{}, err
	}
	out := Response{}
	var b strings.Builder
	for _, o := range v.Output {
		switch o.Type {
		case "message":
			for _, c := range o.Content {
				if c.Type == "output_text" || c.Type == "text" {
					b.WriteString(c.Text)
				}
			}
		case "function_call":
			out.ToolCalls = append(out.ToolCalls, ToolCall{ID: o.CallID, Name: o.Name, Args: o.Arguments})
		}
	}
	out.Text = b.String()
	if out.Text == "" {
		out.Text = v.OutputText
	}
	return out, nil
}
