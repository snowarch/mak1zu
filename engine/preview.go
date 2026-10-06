package engine

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
)

// PreviewTurn is one line of a panel test conversation.
type PreviewTurn struct {
	Role string `json:"role"` // "you" or "her"
	Text string `json:"text"`
}

// PreviewResult is her answer plus the exact prompt that produced it, so a
// persona or rule edit can be judged by reading both.
type PreviewResult struct {
	Reply     string   `json:"reply"`
	System    string   `json:"system"`
	Model     string   `json:"model"`
	LatencyMs int64    `json:"latency_ms"`
	Words     int      `json:"words"`
	Robotic   []string `json:"robotic_hits,omitempty"`
}

// Preview answers as the active persona would, with the live rules, skills and
// mood, but without tools, memory writes or any platform side effect.
func (e *Engine) Preview(ctx context.Context, speaker string, convo []PreviewTurn) (PreviewResult, error) {
	if len(convo) == 0 || convo[len(convo)-1].Role != "you" {
		return PreviewResult{}, errors.New("say something first")
	}
	cfg := e.Cfg.Get()
	pa, err := e.personaCfg()
	if err != nil {
		return PreviewResult{}, err
	}
	if speaker = strings.TrimSpace(speaker); speaker == "" {
		speaker = "You"
	}
	pctx := persona.Context{
		Now: time.Now().Format("Monday 2 January 2006, 15:04 MST"), Platform: "the panel",
		Speaker: speaker, Place: "a private test chat in the control panel",
		LanguageHint: languageHint(cfg.Language),
	}
	if pa.Mood {
		pctx.Mood = e.mood.Describe()
	}
	pctx.Rules = e.Home().Directives("", "")
	for _, sk := range e.Home().SkillList() {
		line := sk.Name
		if sk.Description != "" {
			line += ": " + sk.Description
		}
		pctx.Skills = append(pctx.Skills, line)
	}
	system := persona.Compose(pa, pctx)

	var msgs []provider.Message
	for _, t := range convo {
		if t.Role == "her" {
			msgs = append(msgs, provider.Message{Role: provider.Assistant, Content: t.Text})
		} else {
			msgs = append(msgs, provider.Message{Role: provider.User, Content: label(speaker, t.Text)})
		}
	}
	req := provider.Request{System: system, Messages: msgs, MaxTokens: cfg.Behavior.Turn.MaxTokens}
	if pa.Temperature > 0 {
		x := pa.Temperature
		req.Temperature = &x
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	start := time.Now()
	resp, err := e.LLM.Complete(ctx, req)
	if err != nil {
		return PreviewResult{System: system}, err
	}
	out, v := guard.Clean(resp.Text)
	if v != guard.OK {
		return PreviewResult{System: system}, errors.New("the model answered with something the guard would never send: " + v.String())
	}
	out = guard.LimitEmojis(out, cfg.Behavior.Turn.EmojiBudget)
	return PreviewResult{
		Reply: out, System: system, Model: strings.Trim(resp.Provider+" / "+resp.Model, " /"),
		LatencyMs: time.Since(start).Milliseconds(), Words: len(strings.Fields(out)), Robotic: guard.RoboticHits(out),
	}, nil
}
