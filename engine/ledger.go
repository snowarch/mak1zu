package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/snowarch/mak1zu/memory"
)

// ago says how long it has been in the way a person would, not a timestamp.
func ago(then, now time.Time) string {
	d := now.Sub(then)
	switch {
	case d < 20*time.Hour:
		return "today"
	case d < 44*time.Hour:
		return "yesterday"
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24+0.5))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/24/7+0.5))
	default:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30+0.5))
	}
}

// describeMemory is one recalled memory with its provenance: how long ago, and
// where, when that is not the place they are talking now. This is what lets her
// say "you told me on the terminal" and be right.
func describeMemory(m memory.Memory, here string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, m.Created)
	if err != nil {
		return m.Content
	}
	when := ago(t, now)
	if m.Source != "" && m.Source != here {
		return fmt.Sprintf("%s (told on %s, %s)", m.Content, m.Source, when)
	}
	return fmt.Sprintf("%s (%s)", m.Content, when)
}

// ledgerLines are the open threads and the bits ready for a callback, as the
// lines that go into the prompt. Small by construction: five threads, two bits.
func (e *Engine) ledgerLines(ctx context.Context, personaID string, per memory.Person) (threads, bits []string) {
	now := time.Now()
	ts, _ := e.Mem.OpenThreads(ctx, per.ID, 5)
	for _, t := range ts {
		line := fmt.Sprintf("#%d %s", t.ID, t.Text)
		if due, err := time.Parse(time.RFC3339, t.Due); err == nil {
			line += " (due " + due.Local().Format("Mon 2 Jan") + ")"
		}
		threads = append(threads, line)
	}
	bs, _ := e.Mem.BitsReady(ctx, personaID, per.ID, now, 2)
	for _, b := range bs {
		bits = append(bits, b.Text)
	}
	return
}

// ledgerView is what /memories shows: everything she holds about this person,
// so it can be read and pruned. Nothing here is hidden from them.
func (e *Engine) ledgerView(ctx context.Context, personaID string, per memory.Person) string {
	var b strings.Builder
	now := time.Now()
	ms, _ := e.Mem.Recall(ctx, personaID, per.ID, "", 10)
	for _, m := range ms {
		fmt.Fprintf(&b, "#%d %s\n", m.ID, describeMemory(m, e.Tr.Name(), now))
	}
	if ts, _ := e.Mem.OpenThreads(ctx, per.ID, 12); len(ts) > 0 {
		b.WriteString("\nopen threads (forget with `thread N`):\n")
		for _, t := range ts {
			fmt.Fprintf(&b, "t%d %s\n", t.ID, t.Text)
		}
	}
	if bits, _ := e.Mem.Bits(ctx, personaID, per.ID); len(bits) > 0 {
		b.WriteString("\nrunning bits (forget with `bit N`):\n")
		for _, x := range bits {
			fmt.Fprintf(&b, "b%d %s\n", x.ID, x.Text)
		}
	}
	if !per.WantsCheckins() {
		b.WriteString("\nshe will not start conversations with you.\n")
	}
	if per.Quiet != "" {
		b.WriteString("quiet hours: " + per.Quiet + "\n")
	}
	if b.Len() == 0 {
		return "nothing yet"
	}
	return b.String()
}
