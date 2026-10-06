// Command mak1zu runs and manages a Mak1zu companion.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	makizu "github.com/snowarch/mak1zu"
	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/engine"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/mcpclient"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/panel"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/transport/cli"
	"github.com/snowarch/mak1zu/transport/discord"
)

var version = "0.1.0-dev"

const usage = `mak1zu %s: a companion you can shape.

Usage:
  mak1zu init [dir]            create dir/.makizu with config, personas, rules and skills (default .)
  mak1zu run                   start the companion (Discord + web panel)
  mak1zu chat                  talk to her in the terminal
  mak1zu doctor [--offline]    check config, persona, memory and make a real call to each provider
  mak1zu persona list|check    list personas / validate the active one
  mak1zu eval <inputs.txt>     score the active persona's voice on a list of inputs
  mak1zu service               print a systemd user unit
  mak1zu version

Global flag: -c <config.json> (default: $MAK1ZU_CONFIG, then .makizu/config.json found by walking up from here)
`

func main() {
	cfgPath := flag.String("c", "", "config file")
	flag.Usage = func() { fmt.Fprintf(os.Stderr, usage, version) }
	flag.Parse()
	provider.UserAgent = "mak1zu/" + version + " (+https://github.com/snowarch/mak1zu)"
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	var err error
	switch args[0] {
	case "version":
		fmt.Println("mak1zu", version)
	case "init":
		err = cmdInitArgs(args[1:])
	case "service":
		err = cmdService(*cfgPath)
	case "run", "chat", "doctor", "persona", "eval":
		var st *config.Store
		if st, err = loadConfig(*cfgPath); err != nil {
			break
		}
		switch args[0] {
		case "run":
			err = cmdRun(st)
		case "chat":
			err = cmdChat(st)
		case "doctor":
			err = cmdDoctor(st, len(args) > 1 && args[1] == "--offline")
		case "persona":
			err = cmdPersona(st, args[1:])
		case "eval":
			if len(args) < 2 {
				err = errors.New("usage: mak1zu eval <inputs.txt>  (one message per line)")
			} else {
				err = cmdEval(st, args[1])
			}
		}
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mak1zu:", err)
		os.Exit(1)
	}
}

func findConfig(explicit string) (string, error) {
	cands := []string{explicit, os.Getenv("MAK1ZU_CONFIG")}
	// like git: walk up from the working directory looking for .makizu/
	if wd, err := os.Getwd(); err == nil {
		for d := wd; ; d = filepath.Dir(d) {
			cands = append(cands, filepath.Join(d, ".makizu", "config.json"))
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		cands = append(cands, filepath.Join(h, ".config", "mak1zu", ".makizu", "config.json"))
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("no .makizu/config.json found: run `mak1zu init` first")
}

func loadConfig(explicit string) (*config.Store, error) {
	p, err := findConfig(explicit)
	if err != nil {
		return nil, err
	}
	loadDotEnv(filepath.Join(filepath.Dir(p), ".env"))
	return config.Load(p)
}

// loadDotEnv reads KEY=VALUE lines from the .env next to config.json. Variables
// already in the environment win, so systemd or an export always override it.
func loadDotEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		k, v = strings.TrimSpace(strings.TrimPrefix(k, "export ")), strings.Trim(strings.TrimSpace(v), `"'`)
		if ok && k != "" && v != "" && os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func cmdInitArgs(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	preset := fs.String("provider", "", "provider preset id (openai, anthropic, gemini, openrouter, groq, deepseek, mistral, opencode-go, ollama, lmstudio)")
	key := fs.String("key", "", "API key for the preset (prefer the hidden prompt: flags land in shell history)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	var pr provider.Preset
	switch {
	case *preset != "":
		p, ok := provider.PresetByID(*preset)
		if !ok {
			return fmt.Errorf("unknown provider %q", *preset)
		}
		pr = p
	case isTTY(os.Stdin):
		p, k, err := chooseProvider()
		if err != nil {
			return err
		}
		pr, *key = p, k
	}
	return cmdInit(dir, pr, *key)
}

func cmdInit(dir string, pr provider.Preset, key string) error {
	home := filepath.Join(dir, ".makizu")
	if err := os.MkdirAll(filepath.Join(home, "data"), 0o700); err != nil {
		return err
	}
	cfgPath := filepath.Join(home, "config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", cfgPath)
	}
	cfg := config.Default()
	keyEnv := "MAK1ZU_API_KEY"
	if pr.ID != "" {
		cfg.LLM.Providers["main"] = pr.Provider()
		keyEnv = pr.KeyEnv
	}
	if err := config.New(cfgPath, cfg).Save(cfg); err != nil {
		return err
	}
	copied, kept := 0, 0
	err := fs.WalkDir(makizu.Defaults, ".makizu", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		dst := filepath.Join(dir, p)
		if _, err := os.Stat(dst); err == nil {
			kept++ // never overwrite what the user already wrote
			return nil
		}
		b, _ := makizu.Defaults.ReadFile(p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		copied++
		return os.WriteFile(dst, b, 0o644)
	})
	if err != nil {
		return err
	}
	env := "# Secrets. Never commit this file. Load with: set -a; . ./.makizu/.env; set +a\n"
	if keyEnv != "" {
		env += keyEnv + "=" + key + "\n"
	}
	env += "MAK1ZU_DISCORD_TOKEN=\n"
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte(env), 0o600); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(home, ".gitignore"), []byte("config.json\n.env\ndata/\n"), 0o644)
	fmt.Printf("created %s (%d default files, %d kept)\n\nnext:\n  1. put your API key in %s/.env (and a Discord bot token for Discord)\n  2. mak1zu doctor\n  3. mak1zu chat      (try her in the terminal)\n  4. mak1zu run       (panel at http://127.0.0.1:8787)\n\nher rules, skills and personalities live in %s: edit them by hand or in the panel.\n", home, copied, kept, home, home)
	return nil
}

// singleInstance refuses a second process on the same data dir: duplicate
// Discord clients mean double replies.
func singleInstance(dataDir string) (func(), error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "mak1zu.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another mak1zu is already running on this data directory")
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}

func build(st *config.Store, tr sdk.Transport) (*engine.Engine, *memory.Store, *provider.Router, error) {
	cfg := st.Get()
	mem, err := memory.Open(st.Abs(cfg.Memory.Path))
	if err != nil {
		return nil, nil, nil, err
	}
	router := provider.NewRouter(st.Get)
	e := engine.New(st, router, mem, persona.Library{Dir: st.Abs(cfg.Persona.Dir)}, tr)
	e.Tel = telemetry.New(500, filepath.Join(filepath.Dir(st.Abs(cfg.Memory.Path)), "telemetry.jsonl"))
	return e, mem, router, nil
}

func cmdRun(st *config.Store) error {
	cfg := st.Get()
	unlock, err := singleInstance(filepath.Dir(st.Abs(cfg.Memory.Path)))
	if err != nil {
		return err
	}
	defer unlock()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var tr sdk.Transport
	if cfg.Discord.Enabled {
		dt, err := discord.New(cfg.Discord.BotToken())
		if err != nil {
			return err
		}
		tr = dt
	} else {
		slog.Warn("discord.enabled is false: only the web panel will run (use `mak1zu chat` to talk to her)")
		tr = idle{}
	}
	e, mem, router, err := build(st, tr)
	if err != nil {
		return err
	}
	defer mem.Close()
	for name, srv := range cfg.Tools.MCP {
		if !srv.Enabled {
			continue
		}
		ts, closeFn, err := mcpclient.Start(ctx, name, srv)
		if err != nil {
			slog.Warn("mcp server skipped", "name", name, "err", err)
			continue
		}
		defer closeFn()
		for _, t := range ts {
			e.Tools.Add(t)
		}
		slog.Info("mcp server", "name", name, "tools", len(ts))
	}

	if cfg.WebUI.Enabled {
		ps := &panel.Server{Cfg: st, Lib: func() persona.Library { return persona.Library{Dir: st.Abs(st.Get().Persona.Dir)} },
			Home: e.Home, Ev: e.Ev, Mood: e.Mood, Preview: e.Preview, Version: version, Started: time.Now(),
			Mem: mem, Tel: e.Tel, Router: router, Tools: func() []string {
				var n []string
				for _, s := range e.Tools.Specs(true) {
					n = append(n, s.Name)
				}
				return n
			}}
		go func() {
			slog.Info("web panel", "url", fmt.Sprintf("http://%s:%d", cfg.WebUI.Host, cfg.WebUI.Port))
			if err := ps.Serve(ctx); err != nil {
				slog.Error("web panel", "err", err)
				stop()
			}
		}()
	}
	slog.Info("mak1zu online", "persona", cfg.Persona.Active, "version", version)
	if err := e.Run(ctx); err != nil {
		return err
	}
	return nil
}

type idle struct{}

func (idle) Name() string                                  { return "idle" }
func (idle) Self() sdk.Identity                            { return sdk.Identity{ID: "idle"} }
func (idle) Run(ctx context.Context, _ sdk.Handler) error  { <-ctx.Done(); return nil }
func (idle) Send(context.Context, string, sdk.Reply) error { return nil }
func (idle) Typing(context.Context, string) error          { return nil }
func (idle) History(context.Context, string, int) ([]sdk.Message, error) {
	return nil, nil
}

func cmdChat(st *config.Store) error {
	cfg := st.Get()
	pa, err := persona.Library{Dir: st.Abs(cfg.Persona.Dir)}.Load(cfg.Persona.Active)
	if err != nil {
		return err
	}
	tr := cli.New(os.Stdin, os.Stdout, "you", pa.Name)
	e, mem, _, err := build(st, tr)
	if err != nil {
		return err
	}
	defer mem.Close()
	fmt.Printf("talking to %s (ctrl-d to leave)\n", pa.Name)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return tr.Run(ctx, func(ctx context.Context, m sdk.Message) { e.Handle(ctx, m) })
}

func cmdDoctor(st *config.Store, offline bool) error {
	cfg := st.Get()
	ok := true
	check := func(good bool, msg string) {
		mark := "ok  "
		if !good {
			mark, ok = "FAIL", false
		}
		fmt.Printf("[%s] %s\n", mark, msg)
	}
	check(cfg.Validate() == nil, "config is valid")
	_, err := persona.Library{Dir: st.Abs(cfg.Persona.Dir)}.Load(cfg.Persona.Active)
	check(err == nil, fmt.Sprintf("persona %q loads (%v)", cfg.Persona.Active, err))
	seen := map[string]bool{}
	for _, n := range append(append([]string{}, cfg.LLM.Routing.Text...), cfg.LLM.Routing.Vision...) {
		if seen[n] {
			continue
		}
		seen[n] = true
		p := cfg.LLM.Providers[n]
		if offline {
			needs := p.APIKeyEnv != "" || p.APIKey != ""
			check(!needs || p.Key() != "", fmt.Sprintf("provider %q has its key (env %s)", n, p.APIKeyEnv))
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		d := provider.Probe(ctx, n, p)
		cancel()
		if d.OK {
			check(true, fmt.Sprintf("provider %q answers (%s, %s, %dms)", n, p.Model, d.Reply, d.Latency.Milliseconds()))
			continue
		}
		check(false, fmt.Sprintf("provider %q: %s", n, d.Problem))
		fmt.Printf("       fix: %s\n", d.Fix)
		if len(d.Models) > 0 {
			fmt.Printf("       it does serve: %s\n", strings.Join(d.Models, ", "))
		}
	}
	if cfg.Discord.Enabled {
		check(cfg.Discord.BotToken() != "", "discord token present")
		check(cfg.Discord.OwnerID != "", "discord.owner_id set (otherwise anyone can DM her)")
	}
	if _, err := memory.Open(st.Abs(cfg.Memory.Path)); err != nil {
		check(false, "memory db opens: "+err.Error())
	} else {
		check(true, "memory db opens")
	}
	if !ok {
		return errors.New("doctor found problems")
	}
	fmt.Println("all good")
	return nil
}

func cmdPersona(st *config.Store, args []string) error {
	cfg := st.Get()
	lib := persona.Library{Dir: st.Abs(cfg.Persona.Dir)}
	if len(args) == 0 {
		return errors.New("usage: mak1zu persona list|check")
	}
	switch args[0] {
	case "list":
		for _, id := range lib.List() {
			mark := " "
			if id == cfg.Persona.Active {
				mark = "*"
			}
			fmt.Println(mark, id)
		}
	case "check":
		p, err := lib.Load(cfg.Persona.Active)
		if err != nil {
			return err
		}
		sys := persona.Compose(p, persona.Context{Speaker: "Test"})
		fmt.Printf("%s: %d chars of system prompt (~%d tokens)\n", p.Name, len(sys), len(sys)/4)
	default:
		return errors.New("usage: mak1zu persona list|check")
	}
	return nil
}

func cmdService(explicit string) error {
	p, err := findConfig(explicit)
	if err != nil {
		return err
	}
	abs, _ := filepath.Abs(p)
	exe, _ := os.Executable()
	unit := map[string]string{"exe": exe, "cfg": abs, "dir": filepath.Dir(abs)}
	b, _ := json.Marshal(unit)
	_ = b
	fmt.Printf(`# ~/.config/systemd/user/mak1zu.service   (then: systemctl --user enable --now mak1zu)
[Unit]
Description=Mak1zu companion
After=network-online.target

[Service]
ExecStart=%s -c %s run
WorkingDirectory=%s
EnvironmentFile=-%s/.env
Restart=on-failure
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=%s
PrivateTmp=yes

[Install]
WantedBy=default.target
`, exe, abs, filepath.Dir(abs), filepath.Dir(abs), filepath.Dir(abs))
	return nil
}
