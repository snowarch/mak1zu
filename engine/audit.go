package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/snowarch/mak1zu/guard"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/sdk"
)

var (
	reminderPromise = regexp.MustCompile(`(?i)\b(i'?ll remind you|reminder (is )?set|i set (a|the) reminder|te (lo )?recordar[eé]|te aviso (en|a las|mañana))\b`)
	filePromise     = regexp.MustCompile(`(?i)\b(attached|here'?s the file|the (complete )?file is attached|adjunto|te dejo el archivo)\b`)
)

// audit compares what a finished reply claims with what the turn did. A gap
// becomes an unmet_promise incident the next turn can see: a companion that
// says "done" and did nothing loses trust faster than one that says "no".
func (e *Engine) audit(m sdk.Message, reply string, toolsUsed []string, files int) {
	used := map[string]bool{}
	for _, t := range toolsUsed {
		used[t] = true
	}
	var gap string
	switch {
	case reminderPromise.MatchString(reply) && !used["set_reminder"]:
		gap = "said a reminder was set but set_reminder never ran"
	case filePromise.MatchString(reply) && files == 0:
		gap = "said a file was attached but nothing was attached"
	}
	if gap == "" {
		return
	}
	e.Inc.Record(m.ChannelID, CauseUnmetPromise, gap)
	e.Tel.Add(telemetry.Record{Kind: "incident", Channel: m.ChannelID, Cause: string(CauseUnmetPromise), Detail: gap})
}

// selfReview is the review_myself tool: her recent replies, tics and open
// failures in this channel, as data she can act on.
func (e *Engine) selfReview(channel string) string {
	e.mu.Lock()
	rs := append([]string(nil), e.recent[channel]...)
	e.mu.Unlock()
	if len(rs) == 0 {
		return "no replies of mine recorded in this channel yet"
	}
	actions, words := 0, 0
	for _, r := range rs {
		if guard.OpensWithAction(r) {
			actions++
		}
		words += len(strings.Fields(r))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "last %d replies: avg %d words, %d opened with an action beat\n", len(rs), words/len(rs), actions)
	for i, r := range rs {
		if len(r) > 140 {
			r = r[:140] + "…"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, r)
	}
	if blk := e.Inc.Block(channel); blk != "" {
		b.WriteString(blk)
	}
	return b.String()
}
