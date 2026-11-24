package main

import (
	"path/filepath"
	"testing"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/transport/discord"
)

// TestEngineCommandsPassDiscordValidation passes the real engine commands (with
// the shipped default personas) through Discord's command builder so any future
// schema violation fails `make check` even without a live Discord run.
func TestEngineCommandsPassDiscordValidation(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, ".makizu")
	defaults, err := defaultsFS()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := home.SyncDefaults(defaults, root, home.SyncOptions{InitOnly: true}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Memory.Path = filepath.Join(root, "data", "memory.db")
	st := config.New(filepath.Join(root, "config.json"), cfg)
	e, mem, _, err := build(st, idle{})
	if err != nil {
		t.Fatal(err)
	}
	defer mem.Close()

	defs, err := discord.BuildCommands(e.Commands())
	if err != nil {
		t.Fatalf("engine commands fail Discord validation: %v", err)
	}
	if len(defs) == 0 {
		t.Fatal("expected slash commands, got none")
	}
}
