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
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/internal/telemetry"
	"github.com/snowarch/mak1zu/mcpclient"
	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/panel"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
	"github.com/snowarch/mak1zu/tools"
	"github.com/snowarch/mak1zu/transport/cli"
	"github.com/snowarch/mak1zu/transport/discord"
)

var version = "0.1.0-dev"

const usage = `mak1zu %s: a companion you can shape.

Usage:
  mak1zu init [flags] [dir]    create dir/.makizu with config, personas, rules and skills (default .);
                               on a terminal it asks which provider (a preset, a server it finds running
                               here, or your own OpenAI-compatible URL) and checks that it answers.
                               without a terminal, pick with flags:
                                 --provider ID                a preset from "mak1zu providers"
                                 --base-url URL [--model ID]  any OpenAI-compatible endpoint
                                 --key KEY | --key-env VAR    the key, or the variable that holds it
                                 --protocol, --header, --vision, --no-check (see: mak1zu init -h)
  mak1zu init --update [--dry-run] [--force] [dir]
                               bring an existing .makizu up to this version's personas, rules and skills
                               without overwriting what you changed (--dry-run looks first)
  mak1zu providers             list presets (cost, model, where to get a key) and servers running here
  mak1zu run                   start the companion (Discord + web panel)
  mak1zu chat                  talk to her in the terminal
  mak1zu doctor [--offline]    check config, persona, memory and make a real call to each provider
  mak1zu persona list|check    list personas / validate the active one
  mak1zu eval <inputs.txt>     score the active persona's voice on a list of inputs
  mak1zu link [CODE]           make the terminal the same person as your Discord account: run /link there
                               for a code and pass it here, or run without a code to get one for the other side
  mak1zu service               print a systemd user unit
  mak1zu version

Global flag: -c <config.json> (default: $MAK1ZU_CONFIG, then .makizu/config.json found by walking up from here)
`

func main() {
	cfgPath := flag.String("c", "", "config file")
	flag.Usage = func() { fmt.Fprintf(os.Stderr, usage, version) }
	flag.Parse()
	provider.UserAgent = "mak1zu/" + version + " (+https://github.com/snowarch/mak1zu)"
	tools.UserAgent = provider.UserAgent
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
	case "providers":
		cmdProviders()
	case "service":
		err = cmdService(*cfgPath)
	case "run", "chat", "doctor", "persona", "eval", "link":
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
		case "link":
			err = cmdLink(st, args[1:])
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
	where := "."
	if wd, err := os.Getwd(); err == nil {
		where = wd
	}
	return "", fmt.Errorf("no .makizu/config.json found in %s or any folder above it, nor in ~/.config/mak1zu, and MAK1ZU_CONFIG is not set.\nrun `mak1zu init` here to make one, cd into the folder that has your .makizu, or point to it: export MAK1ZU_CONFIG=/path/to/.makizu/config.json", where)
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

// listFlag is a repeatable string flag.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ", ") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func cmdInitArgs(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	var o initOptions
	var headers listFlag
	fs.StringVar(&o.Provider, "provider", "", "provider preset id (\"mak1zu providers\" lists them), or custom")
	fs.StringVar(&o.BaseURL, "base-url", "", "any OpenAI-compatible endpoint, the address before /chat/completions (http://localhost:8000/v1)")
	fs.StringVar(&o.Model, "model", "", "model id (a preset has a default; a custom endpoint with one model picks it)")
	fs.StringVar(&o.Key, "key", "", "API key (prefer the hidden prompt: flags land in shell history)")
	fs.StringVar(&o.KeyEnv, "key-env", "", "environment variable that already holds the key, so none is written to disk")
	fs.StringVar(&o.Protocol, "protocol", "", "chat (default) or responses")
	fs.Var(&headers, "header", "extra request header, \"Name: value\" (repeatable)")
	fs.BoolVar(&o.Vision, "vision", false, "the model accepts images")
	fs.BoolVar(&o.NoCheck, "no-check", false, "do not make the live check call after writing the config")
	update := fs.Bool("update", false, "update an existing .makizu to this version's defaults, keeping your edits")
	dry := fs.Bool("dry-run", false, "with --update: show what would change and write nothing")
	force := fs.Bool("force", false, "with --update: overwrite files you changed (the old copy is kept in .makizu/.backup)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o.Headers = headers
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if *update {
		return cmdUpdate(dir, fs.NArg() > 0, *dry, *force)
	}
	if *dry || *force {
		return errors.New("--dry-run and --force only make sense with --update")
	}
	// refuse before asking anything: nobody wants six questions and then a no
	if _, err := os.Stat(filepath.Join(dir, ".makizu", "config.json")); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite (change it in the panel, or `mak1zu init --update` for new defaults)", filepath.Join(dir, ".makizu", "config.json"))
	}
	ctx := context.Background()
	w := newWizard()
	var ch choice
	var err error
	switch {
	case o.any():
		if ch, err = w.fromFlags(ctx, o); err != nil {
			return err
		}
		if !o.NoCheck {
			ch = w.checkOnce(ctx, ch)
		}
	case isTTY(os.Stdin):
		if ch, err = w.choose(ctx); err != nil {
			return err
		}
	}
	return cmdInit(dir, ch)
}

func cmdInit(dir string, ch choice) error {
	root := filepath.Join(dir, ".makizu")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o700); err != nil {
		return err
	}
	cfgPath := filepath.Join(root, "config.json")
	if _, err := os.Stat(cfgPath); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", cfgPath)
	}
	cfg := config.Default()
	keyEnv := "MAK1ZU_API_KEY"
	if ch.ID != "" {
		cfg.LLM.Providers["main"] = ch.P
		keyEnv = ch.P.APIKeyEnv
	}
	if err := config.New(cfgPath, cfg).Save(cfg); err != nil {
		return err
	}
	defaults, err := defaultsFS()
	if err != nil {
		return err
	}
	changes, err := home.SyncDefaults(defaults, root, home.SyncOptions{InitOnly: true, Version: version})
	if err != nil {
		return err
	}
	copied, kept := 0, 0
	for _, c := range changes {
		if c.Action == home.Added {
			copied++
		} else {
			kept++ // never overwrite what the user already wrote
		}
	}
	env := "# Secrets. Never commit this file. Load with: set -a; . ./.makizu/.env; set +a\n"
	if keyEnv != "" && (ch.Key != "" || os.Getenv(keyEnv) == "") {
		env += keyEnv + "=" + ch.Key + "\n"
	}
	env += "MAK1ZU_DISCORD_TOKEN=\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(env), 0o600); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(root, ".gitignore"), []byte("config.json\n.env\ndata/\n.updates/\n.backup/\n"), 0o644)
	fmt.Printf("created %s (%d default files, %d kept)\n", root, copied, kept)
	if ch.ID != "" {
		fmt.Printf("provider: %s, model %s\n", ch.P.BaseURL, ch.P.Model)
	}
	fmt.Printf("\nnext:\n")
	step := 1
	say := func(f string, a ...any) { fmt.Printf("  %d. "+f+"\n", append([]any{step}, a...)...); step++ }
	switch {
	case ch.ID == "":
		say("pick the provider: edit %s/config.json, or run `mak1zu run` and use Models in the panel (a preset, or your own URL), then put the key in %s/.env", root, root)
	case keyEnv == "":
		if ch.Note != "" {
			say("%s: %s", ch.Label, ch.Note)
		}
	case ch.Key != "":
		say("key saved in %s/.env as %s", root, keyEnv)
	case os.Getenv(keyEnv) != "":
		say("the key comes from %s in your environment; a service started elsewhere will not see it, so copy it to %s/.env if you run her as one", keyEnv, root)
	case ch.KeyURL != "":
		say("get a key at %s and put it in %s/.env as %s", ch.KeyURL, root, keyEnv)
	default:
		say("put the key in %s/.env as %s", root, keyEnv)
	}
	if ch.ID != "" && ch.Note != "" && keyEnv != "" {
		fmt.Printf("     heads up: %s\n", ch.Note)
	}
	if len(ch.P.Headers) > 0 {
		fmt.Printf("     the extra headers are stored in %s/config.json, which is gitignored: keep secrets out of them if you can\n", root)
	}
	if !ch.Works {
		say("mak1zu doctor    (a real call to the provider; it says what to fix)")
	}
	say("mak1zu chat      (try her in the terminal)")
	say("mak1zu run       (panel at http://127.0.0.1:8787; Discord setup is in docs/SETUP.md)")
	fmt.Printf("\nher rules, skills and personalities live in %s: edit them by hand or in the panel.\n", root)
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
			Home: e.Home, Ev: e.Ev, Mood: e.Mood, MoodState: e.MoodState, Preview: e.Preview, Version: version, Started: time.Now(),
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
				if strings.Contains(err.Error(), "address already in use") {
					slog.Error("web panel: that port is taken by another program (or another mak1zu). Pick a free one: set web_ui.port in .makizu/config.json", "port", cfg.WebUI.Port)
				} else {
					slog.Error("web panel", "err", err)
				}
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

// cmdLink joins the terminal to the person you already are elsewhere, so there
// is one memory: with a code from another platform it links this terminal; with
// none it prints a code for that other platform to use.
func cmdLink(st *config.Store, args []string) error {
	mem, err := memory.Open(st.Abs(st.Get().Memory.Path))
	if err != nil {
		return err
	}
	defer mem.Close()
	ctx := context.Background()
	if len(args) > 0 {
		p, err := mem.Link(ctx, "cli", "user", "you", args[0])
		if err != nil {
			return err
		}
		fmt.Printf("linked: the terminal is %s now, one memory\n", p.Display())
		return nil
	}
	p, err := mem.Resolve(ctx, "cli", "user", "you")
	if err != nil {
		return err
	}
	code, err := mem.NewLinkCode(ctx, p.ID)
	if err != nil {
		return err
	}
	fmt.Printf("code %s (10 minutes, one use)\nOn the other account say: /link %s\n", code, code)
	return nil
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
	if err != nil {
		check(false, fmt.Sprintf("persona %q does not load: %v", cfg.Persona.Active, err))
	} else {
		check(true, fmt.Sprintf("persona %q loads", cfg.Persona.Active))
	}
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
	} else {
		fmt.Println("[skip] discord is off: terminal and panel only (docs/SETUP.md has the steps to put her in a server)")
	}
	if defaults, err := defaultsFS(); err == nil {
		if cs, err := home.SyncDefaults(defaults, filepath.Dir(st.Path()), home.SyncOptions{DryRun: true}); err == nil {
			if apply, decide := home.Pending(cs); apply+decide > 0 {
				fmt.Printf("[note] %d personality/rule/skill file(s) changed in this version (%d need a decision): `mak1zu init --update --dry-run` shows them\n", apply+decide, decide)
			}
		}
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

// defaultsFS is the personas, rules and skills this binary ships, rooted at
// the contents of .makizu.
func defaultsFS() (fs.FS, error) { return fs.Sub(makizu.Defaults, ".makizu") }

// cmdUpdate brings an existing .makizu up to this version's defaults. It never
// overwrites a file the person changed: those get the new version staged next
// to them under .updates/ for comparison.
func cmdUpdate(dir string, explicitDir, dry, force bool) error {
	root := filepath.Join(dir, ".makizu")
	if !explicitDir {
		p, err := findConfig("")
		if err != nil {
			return err
		}
		root = filepath.Dir(p)
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); err != nil {
		return fmt.Errorf("%s has no config.json: run `mak1zu init` first", root)
	}
	defaults, err := defaultsFS()
	if err != nil {
		return err
	}
	changes, err := home.SyncDefaults(defaults, root, home.SyncOptions{DryRun: dry, Force: force, Version: version})
	if err != nil {
		return err
	}
	verb := map[home.Action]string{home.Added: "added", home.Updated: "updated", home.Replaced: "replaced", home.Conflict: "kept yours"}
	if dry {
		verb = map[home.Action]string{home.Added: "would add", home.Updated: "would update", home.Replaced: "would replace", home.Conflict: "would keep yours"}
	}
	tally := map[home.Action]int{}
	for _, c := range changes {
		tally[c.Action]++
		switch c.Action {
		case home.Same:
		case home.Customized:
		default:
			line := fmt.Sprintf("  %-17s %s", verb[c.Action], c.Path)
			if c.Action == home.Removed {
				line = fmt.Sprintf("  %-17s %s", "skipped", c.Path)
			}
			if c.Note != "" {
				line += "\n  " + fmt.Sprintf("%-17s %s", "", c.Note)
			}
			fmt.Println(line)
		}
	}
	fmt.Printf("\n%s (mak1zu %s): %d up to date, %d of yours left alone", root, version, tally[home.Same], tally[home.Customized])
	if n := tally[home.Added] + tally[home.Updated] + tally[home.Replaced]; n > 0 {
		fmt.Printf(", %d %s", n, map[bool]string{true: "to change", false: "changed"}[dry])
	}
	fmt.Println()
	if n := tally[home.Conflict]; n > 0 && !force && dry {
		fmt.Printf("%d file(s) differ from this release and from what you started with. A real --update leaves yours untouched and saves the new version next to it in .updates/ so you can compare.\n", n)
	} else if n > 0 && !force {
		fmt.Printf("%d file(s) differ from this release and from what you started with. Yours are untouched; compare with:\n  diff -u %s/<file> %s/.updates/<file>\nor take the new one with --force (the old copy goes to .backup/).\n", n, root, root)
	}
	if !dry && tally[home.Added]+tally[home.Updated]+tally[home.Replaced] > 0 {
		fmt.Println("personas, rules and skills are re-read on every message: no restart needed.")
	}
	return nil
}
