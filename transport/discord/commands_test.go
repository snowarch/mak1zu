package discord

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"github.com/snowarch/mak1zu/sdk"
)

func TestBuildCommandsValidatesDiscordLimits(t *testing.T) {
	valid := []sdk.Command{
		{Name: "ping", Description: strings.Repeat("a", 100)},
		{Name: "persona", Description: "Switch character", Options: []sdk.CommandOption{
			{Name: "id", Description: "persona id", Required: true, Choices: []string{"maki", "neutral"}},
			{Name: "note", Description: "optional note", Required: false},
		}},
	}
	defs, err := BuildCommands(valid)
	if err != nil || len(defs) != 2 {
		t.Fatalf("valid commands rejected: %v (len=%d)", err, len(defs))
	}

	// Regression: a 107-char /link description made Discord reject the whole bulk overwrite.
	if _, err := BuildCommands([]sdk.Command{{Name: "link", Description: strings.Repeat("x", 101)}}); err == nil {
		t.Fatal("accepted 101-char command description")
	}
	if _, err := BuildCommands([]sdk.Command{{Name: "BadName", Description: "ok"}}); err == nil {
		t.Fatal("accepted uppercase command name")
	}
	if _, err := BuildCommands([]sdk.Command{{Name: "dup", Description: "a"}, {Name: "dup", Description: "b"}}); err == nil {
		t.Fatal("accepted duplicate command name")
	}
	if _, err := BuildCommands([]sdk.Command{{Name: "x", Description: "ok", Options: []sdk.CommandOption{
		{Name: "opt", Description: "first", Required: false},
		{Name: "req", Description: "second", Required: true},
	}}}); err == nil {
		t.Fatal("accepted required option after optional option")
	}
	if _, err := BuildCommands([]sdk.Command{{Name: "x", Description: "ok", Options: []sdk.CommandOption{
		{Name: "arg", Description: strings.Repeat("y", 101)},
	}}}); err == nil {
		t.Fatal("accepted 101-char option description")
	}
}

func TestBuildCommandsCapsChoicesAt25(t *testing.T) {
	choices := make([]string, 30)
	for i := range choices {
		choices[i] = "p" + strings.Repeat("a", i+1)
	}
	defs, err := BuildCommands([]sdk.Command{{
		Name: "persona", Description: "pick",
		Options: []sdk.CommandOption{{Name: "id", Description: "id", Choices: choices}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(defs[0].Options[0].Choices); got != 25 {
		t.Fatalf("choices = %d, want 25", got)
	}
}

func TestClipReplyPreservesUTF8AndNonEmpty(t *testing.T) {
	if got := clipReply("   "); got == "" {
		t.Fatal("empty reply must fall back to non-empty placeholder")
	}
	long := strings.Repeat("ñ", 2000)
	got := clipReply(long)
	if !utf8.ValidString(got) {
		t.Fatal("clipReply produced invalid UTF-8")
	}
	if n := utf8.RuneCountInString(got); n != 1901 {
		t.Fatalf("rune count = %d, want 1901", n)
	}
}

func TestFlushCommandsUsesAppIDAndRetriesAfterFailure(t *testing.T) {
	calls := 0
	var gotAppID string
	tr := &Transport{
		self:    sdk.Identity{ID: "user-id", Name: "Maki"},
		appID:   "app-id",
		pending: []*discordgo.ApplicationCommand{{Name: "ping", Description: "check"}},
		overwrite: func(appID string, defs []*discordgo.ApplicationCommand) error {
			calls++
			gotAppID = appID
			if calls == 1 {
				return errors.New("temporary 503")
			}
			return nil
		},
	}
	if err := tr.flushCommands(); err == nil {
		t.Fatal("first flush should fail")
	}
	if gotAppID != "app-id" {
		t.Fatalf("appID = %q, want app-id", gotAppID)
	}
	if tr.pending == nil {
		t.Fatal("pending cleared on failure; reconnect will not retry")
	}
	if err := tr.flushCommands(); err != nil {
		t.Fatalf("second flush should succeed: %v", err)
	}
	if tr.pending != nil {
		t.Fatal("pending not cleared after success")
	}
	if err := tr.flushCommands(); err != nil || calls != 2 {
		t.Fatalf("third flush should be a no-op: err=%v calls=%d", err, calls)
	}
}
