package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/provider"
)

func isTTY(f *os.File) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}

// readSecret reads a line from the terminal without echoing it.
func readSecret(r *bufio.Reader) string {
	fd := os.Stdin.Fd()
	var old syscall.Termios
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCGETS, uintptr(unsafe.Pointer(&old))); e == 0 {
		quiet := old
		quiet.Lflag &^= syscall.ECHO
		syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&quiet)))
		defer func() {
			syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TCSETS, uintptr(unsafe.Pointer(&old)))
			fmt.Println()
		}()
	}
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}

// choice is everything init needs to write a provider: the config block, the
// key to put in .env (never in config.json), and what the live check found.
type choice struct {
	ID     string // preset id, "custom", or "" when nothing was chosen
	Label  string
	Note   string
	KeyURL string
	P      config.Provider
	Key    string // pasted by the person; goes to .makizu/.env
	Works  bool   // the live check got an answer
}

// wizard asks the questions. Everything it touches from outside (terminal,
// network, environment) is a field, so the whole conversation is testable.
type wizard struct {
	ui      ui // nil unless both ends are a terminal; then questions are asked with arrow keys
	in      *bufio.Reader
	out     io.Writer
	secret  func() string
	getenv  func(string) string
	detect  func(context.Context) []provider.Local
	resolve func(ctx context.Context, raw, key string) provider.Resolved
	list    func(context.Context, config.Provider) ([]string, int, error)
	probe   func(context.Context, string, config.Provider) provider.Diagnosis
}

func newWizard() *wizard {
	r := bufio.NewReader(os.Stdin)
	var u ui
	if isTTY(os.Stdin) && isTTY(os.Stdout) {
		u = huhUI{}
	}
	return &wizard{
		ui: u,
		in: r, out: os.Stdout,
		secret:  func() string { return readSecret(r) },
		getenv:  os.Getenv,
		detect:  provider.DetectLocal,
		resolve: provider.ResolveEndpoint,
		list:    provider.ListModels,
		probe:   provider.Probe,
	}
}

func (w *wizard) say(f string, a ...any) { fmt.Fprintf(w.out, f+"\n", a...) }

func (w *wizard) ask(prompt string) (string, error) {
	if w.ui != nil {
		return w.ui.input(prompt, false)
	}
	fmt.Fprint(w.out, prompt)
	line, err := w.in.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return "", errors.New("no answer (input closed); for scripts use flags, see: mak1zu init -h")
	}
	return strings.TrimSpace(line), nil
}

func (w *wizard) askSecret(prompt string) string {
	if w.ui != nil {
		v, _ := w.ui.input(prompt, true)
		return v
	}
	fmt.Fprint(w.out, prompt)
	return w.secret()
}

type menuRow struct {
	preset *provider.Preset
	local  *provider.Local
	custom bool
}

// choose runs the whole interactive setup: which provider, the key, the
// model, and a real call to prove it works.
func (w *wizard) choose(ctx context.Context) (choice, error) {
	locals := w.detect(ctx)
	var rows []menuRow
	for i := range provider.Presets {
		rows = append(rows, menuRow{preset: &provider.Presets[i]})
	}
	for i := range locals {
		if !matchesPreset(locals[i]) {
			rows = append(rows, menuRow{local: &locals[i]})
		}
	}
	rows = append(rows, menuRow{custom: true})

	var row menuRow
	var prefill string
	if w.ui != nil {
		labels := make([]string, len(rows))
		for i, r := range rows {
			l, d := w.describe(r, locals)
			labels[i] = fmt.Sprintf("%-28s %s", l, d)
		}
		i, err := w.ui.pick("which model provider?  (type to filter; the last entry is any OpenAI-compatible URL)", labels)
		if err != nil {
			return choice{}, err
		}
		row = rows[i]
	} else {
		w.say("which model provider? pick a number, a preset id, or paste your own URL.")
		w.say("(your own server, a gateway, any OpenAI-compatible API: the last entry.)")
		for i, r := range rows {
			label, detail := w.describe(r, locals)
			w.say("  %2d  %-30s %s", i+1, label, detail)
		}
	}
	for tries := 0; w.ui == nil; tries++ {
		line, err := w.ask("number, preset id or URL: ")
		if err != nil {
			return choice{}, err
		}
		if n, e := strconv.Atoi(line); e == nil && n >= 1 && n <= len(rows) {
			row = rows[n-1]
			break
		}
		if p, ok := provider.PresetByID(line); ok {
			row = menuRow{preset: &p}
			break
		}
		low := strings.ToLower(line)
		if low == "custom" || low == "other" || low == "url" || low == "own" {
			row = menuRow{custom: true}
			break
		}
		if strings.Contains(line, "://") || (strings.Contains(line, ".") && strings.ContainsAny(line, "/:")) {
			row, prefill = menuRow{custom: true}, line
			break
		}
		if tries >= 2 {
			return choice{}, fmt.Errorf("%q is not on the list; run `mak1zu providers`, or pick the last entry for your own URL", line)
		}
		w.say("  not on the list. a number from 1 to %d, a preset id, or a URL.", len(rows))
	}

	var ch choice
	var err error
	switch {
	case row.custom:
		ch, err = w.custom(ctx, prefill)
	case row.local != nil:
		ch, err = w.localServer(ctx, *row.local)
	default:
		ch, err = w.preset(ctx, *row.preset, locals)
	}
	if err != nil {
		return ch, err
	}
	return w.check(ctx, ch)
}

func matchesPreset(l provider.Local) bool {
	port := portOf(l.BaseURL)
	for _, p := range provider.Presets {
		if provider.IsLocalHost(provider.HostOf(p.BaseURL)) && portOf(p.BaseURL) == port {
			return true
		}
	}
	return false
}

func portOf(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	rest := raw[i+3:]
	if j := strings.Index(rest, "/"); j >= 0 {
		rest = rest[:j]
	}
	if k := strings.LastIndex(rest, ":"); k >= 0 {
		return rest[k+1:]
	}
	return ""
}

func (w *wizard) describe(r menuRow, locals []provider.Local) (string, string) {
	switch {
	case r.custom:
		return "Your own URL", "vLLM, LiteLLM, Together, any OpenAI-compatible API"
	case r.local != nil:
		return r.local.Name + " (local)", fmt.Sprintf("running now on %s, %s", strings.TrimSuffix(strings.TrimPrefix(r.local.BaseURL, "http://"), "/v1"), plural(len(r.local.Models), "model"))
	}
	p := r.preset
	detail := p.Cost
	if provider.IsLocalHost(provider.HostOf(p.BaseURL)) {
		for _, l := range locals {
			if portOf(l.BaseURL) == portOf(p.BaseURL) {
				detail = fmt.Sprintf("running now, %s", plural(len(l.Models), "model"))
			}
		}
	} else if p.KeyEnv != "" && w.getenv(p.KeyEnv) != "" {
		detail += "  [key found: " + p.KeyEnv + "]"
	}
	return p.Label, detail
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func (w *wizard) preset(ctx context.Context, p provider.Preset, locals []provider.Local) (choice, error) {
	ch := choice{ID: p.ID, Label: p.Label, Note: p.Note, KeyURL: p.KeyURL, P: p.Provider()}
	if p.Note != "" {
		w.say("note: %s", p.Note)
	}
	if p.KeyEnv != "" {
		switch {
		case w.getenv(p.KeyEnv) != "":
			w.say("using %s from your environment (not copied to disk).", p.KeyEnv)
		default:
			if p.KeyURL != "" {
				w.say("get a key at %s", p.KeyURL)
			}
			ch.Key = w.askSecret(fmt.Sprintf("paste it (hidden; it only goes into .makizu/.env as %s), or enter to add it later: ", p.KeyEnv))
		}
	}
	var known []string
	if provider.IsLocalHost(provider.HostOf(p.BaseURL)) {
		for _, l := range locals {
			if portOf(l.BaseURL) == portOf(p.BaseURL) {
				known = l.Models
			}
		}
	}
	m, err := w.askModel(ctx, ch, p.Model, known)
	ch.P.Model = m
	return ch, err
}

func (w *wizard) localServer(ctx context.Context, l provider.Local) (choice, error) {
	ch := choice{ID: "custom", Label: l.Name + " (local)", P: provider.Custom(l.BaseURL, "", false)}
	m, err := w.askModel(ctx, ch, "", l.Models)
	ch.P.Model = m
	return ch, err
}

func (w *wizard) custom(ctx context.Context, prefill string) (choice, error) {
	ch := choice{ID: "custom", Label: "your endpoint"}
	def, fromEnv := prefill, false
	if def == "" {
		for _, k := range []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"} {
			if v := w.getenv(k); v != "" {
				def, fromEnv = v, true
				break
			}
		}
	}
	w.say("the address is what comes before /chat/completions, e.g. https://api.together.xyz/v1 or http://localhost:8000/v1")
	var raw string
	for {
		prompt := "address: "
		if def != "" {
			prompt = fmt.Sprintf("address [%s]: ", def)
		}
		line, err := w.ask(prompt)
		if err != nil {
			return ch, err
		}
		if line == "" {
			line = def
		} else {
			fromEnv = false
		}
		if line != "" {
			raw = line
			break
		}
	}
	key := w.askSecret("key (hidden; enter if it needs none): ")
	keyEnv := ""
	if key == "" && fromEnv && w.getenv("OPENAI_API_KEY") != "" {
		keyEnv = "OPENAI_API_KEY"
		w.say("using OPENAI_API_KEY from your environment (not copied to disk).")
	}
	w.say("looking at %s ...", provider.NormalizeBaseURL(raw))
	r := w.resolve(ctx, raw, firstNonEmpty(key, w.getenv(keyEnv)))
	if (r.Status == 401 || r.Status == 403) && keyEnv == "" {
		if key == "" {
			w.say("it answered %d: it wants a key.", r.Status)
			key = w.askSecret("paste it (hidden): ")
		} else {
			w.say("it answered %d: that key was not accepted.", r.Status)
			if again := w.askSecret("paste it again (hidden), or enter to keep this one: "); again != "" {
				key = again
			}
		}
		r = w.resolve(ctx, raw, key)
	}
	switch {
	case r.Err != nil && len(r.Models) == 0:
		w.say("could not read its model list (%v). fine if the server is not up yet; I will keep the address and you type the model.", r.Err)
	case r.Note != "":
		w.say("%s. it serves %s.", r.Note, plural(len(r.Models), "model"))
	default:
		w.say("it serves %s.", plural(len(r.Models), "model"))
	}
	ch.P = provider.Custom(r.BaseURL, keyEnv, false)
	ch.Key = key
	if keyEnv == "" && (key != "" || !provider.IsLocalHost(provider.HostOf(r.BaseURL))) {
		ch.P.APIKeyEnv = provider.KeyEnvFor(r.BaseURL)
	}
	m, err := w.askModel(ctx, ch, "", r.Models)
	if err != nil {
		return ch, err
	}
	ch.P.Model = m
	if w.ui != nil {
		v, err := w.ui.confirm("can this model read images?")
		ch.P.Vision = v
		return ch, err
	}
	if a, err := w.ask("can this model read images? [y/N]: "); err != nil {
		return ch, err
	} else if strings.HasPrefix(strings.ToLower(a), "y") {
		ch.P.Vision = true
	}
	return ch, nil
}

func filterOut(l []string, drop string) []string {
	var out []string
	for _, s := range l {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmpty(a ...string) string {
	for _, s := range a {
		if s != "" {
			return s
		}
	}
	return ""
}

// askModel picks a model. With a list it offers numbers; with a default it
// takes enter; "?" fetches what the endpoint serves.
func (w *wizard) askModel(ctx context.Context, ch choice, def string, models []string) (string, error) {
	if def == "" && len(models) == 1 {
		w.say("using the only model it serves: %s", models[0])
		return models[0], nil
	}
	if w.ui != nil && len(models) > 0 {
		opts := append([]string(nil), models...)
		if def != "" {
			opts = append([]string{def + "  (default)"}, filterOut(models, def)...)
		}
		opts = append(opts, "something else: type a model id")
		i, err := w.ui.pick("which model?", opts)
		if err != nil {
			return "", err
		}
		switch {
		case i == len(opts)-1:
			return w.ui.input("model id", false)
		case def != "" && i == 0:
			return def, nil
		case def != "":
			return filterOut(models, def)[i-1], nil
		}
		return models[i], nil
	}
	show := func() {
		const cap = 30
		for i, m := range models {
			if i == cap {
				w.say("  ... and %d more (type an id)", len(models)-cap)
				break
			}
			w.say("  %2d  %s", i+1, m)
		}
	}
	if def == "" && len(models) > 1 {
		show()
	}
	for {
		var prompt string
		switch {
		case def != "":
			prompt = fmt.Sprintf("model [%s] (enter keeps it, ? lists what it serves): ", def)
		case len(models) > 0:
			prompt = "model (number or id): "
		default:
			prompt = "model id: "
		}
		line, err := w.ask(prompt)
		if err != nil {
			return "", err
		}
		switch {
		case line == "" && def != "":
			return def, nil
		case line == "":
			continue
		case line == "?":
			if len(models) == 0 {
				ids, _, e := w.list(ctx, w.probeConfig(ch))
				if e != nil {
					w.say("  cannot list models here: %v", e)
					continue
				}
				models = ids
			}
			show()
			continue
		}
		if n, e := strconv.Atoi(line); e == nil && n >= 1 && n <= len(models) {
			return models[n-1], nil
		}
		return line, nil
	}
}

// probeConfig is the provider as the live check should see it: the key from
// the environment if it is there, else the one just pasted (it is not in the
// environment yet).
func (w *wizard) probeConfig(ch choice) config.Provider {
	c := ch.P
	c.APIKey = ""
	if c.APIKeyEnv != "" && w.getenv(c.APIKeyEnv) != "" {
		c.APIKey = w.getenv(c.APIKeyEnv)
	} else {
		c.APIKey = ch.Key
	}
	return c
}

func canCheck(c config.Provider) bool {
	return c.APIKeyEnv == "" || c.APIKey != ""
}

// check makes the one real call. On a terminal a failure becomes a short
// repair loop (model, key, address) so a typo is a retry, not a restart.
func (w *wizard) check(ctx context.Context, ch choice) (choice, error) {
	for round := 0; round < 6; round++ {
		c := w.probeConfig(ch)
		if !canCheck(c) {
			w.say("no key yet, so no live check. once it is in .makizu/.env: mak1zu doctor")
			return ch, nil
		}
		w.say("checking that %s answers ...", c.Model)
		pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		d := w.probe(pctx, "main", c)
		cancel()
		if d.OK {
			w.say("yes: %q in %dms.", d.Reply, d.Latency.Milliseconds())
			ch.Works = true
			return ch, nil
		}
		w.say("no: %s", d.Problem)
		w.say("fix: %s", d.Fix)
		if len(d.Models) > 0 {
			w.say("it does serve: %s", strings.Join(d.Models, ", "))
		}
		a, err := w.ask("fix it now? [m]odel  [k]ey  [u]rl  or enter to keep it as is: ")
		if err != nil {
			return ch, nil
		}
		switch strings.ToLower(a) {
		case "m", "model":
			m, err := w.askModel(ctx, ch, "", d.Models)
			if err != nil {
				return ch, nil
			}
			ch.P.Model = m
		case "k", "key":
			ch.Key = w.askSecret("key (hidden): ")
			if ch.P.APIKeyEnv == "" {
				ch.P.APIKeyEnv = provider.KeyEnvFor(ch.P.BaseURL)
			}
		case "u", "url":
			line, err := w.ask("address: ")
			if err != nil || line == "" {
				continue
			}
			r := w.resolve(ctx, line, w.probeConfig(ch).APIKey)
			if r.Note != "" {
				w.say("%s", r.Note)
			}
			ch.P.BaseURL = r.BaseURL
		default:
			return ch, nil
		}
	}
	return ch, nil
}

// checkOnce is the scripted version: one call, the result printed, no questions.
func (w *wizard) checkOnce(ctx context.Context, ch choice) choice {
	c := w.probeConfig(ch)
	if !canCheck(c) {
		w.say("no key yet, so no live check. once it is in .makizu/.env: mak1zu doctor")
		return ch
	}
	w.say("checking that %s answers ...", c.Model)
	pctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	d := w.probe(pctx, "main", c)
	if d.OK {
		w.say("yes: %q in %dms.", d.Reply, d.Latency.Milliseconds())
		ch.Works = true
		return ch
	}
	w.say("no: %s\nfix: %s", d.Problem, d.Fix)
	if len(d.Models) > 0 {
		w.say("it does serve: %s", strings.Join(d.Models, ", "))
	}
	return ch
}

// initOptions are the flags of `mak1zu init` that pick a provider.
type initOptions struct {
	Provider, BaseURL, Model, Key, KeyEnv, Protocol string
	Headers                                         []string
	Vision, NoCheck                                 bool
}

func (o initOptions) any() bool {
	return o.Provider != "" || o.BaseURL != "" || o.Model != "" || o.Key != "" || o.KeyEnv != ""
}

// fromFlags builds the provider without asking anything: a preset, optionally
// overridden, or a custom endpoint when --base-url comes without a preset.
func (w *wizard) fromFlags(ctx context.Context, o initOptions) (choice, error) {
	var ch choice
	custom := o.BaseURL != "" && (o.Provider == "" || strings.EqualFold(o.Provider, "custom"))
	switch {
	case strings.EqualFold(o.Provider, "custom") && o.BaseURL == "":
		return ch, errors.New("--provider custom needs --base-url (the address before /chat/completions, e.g. http://localhost:8000/v1)")
	case o.Provider != "" && !strings.EqualFold(o.Provider, "custom"):
		p, ok := provider.PresetByID(o.Provider)
		if !ok {
			return ch, fmt.Errorf("unknown provider %q: `mak1zu providers` lists them, and --base-url URL --model ID takes any OpenAI-compatible endpoint", o.Provider)
		}
		ch = choice{ID: p.ID, Label: p.Label, Note: p.Note, KeyURL: p.KeyURL, P: p.Provider()}
	case custom:
		ch = choice{ID: "custom", Label: "your endpoint", P: provider.Custom("", "", false)}
	default:
		return ch, errors.New("--model, --key and --key-env need --provider ID or --base-url URL to say where they go")
	}
	ch.Key = o.Key
	var models []string
	if o.BaseURL != "" {
		r := w.resolve(ctx, o.BaseURL, firstNonEmpty(o.Key, w.getenv(o.KeyEnv)))
		if r.BaseURL == "" {
			return ch, errors.New("--base-url is empty")
		}
		if r.Note != "" {
			w.say("%s", r.Note)
		}
		ch.P.BaseURL, models = r.BaseURL, r.Models
		if custom {
			ch.P = provider.Custom(r.BaseURL, "", false)
			if o.Key != "" || !provider.IsLocalHost(provider.HostOf(r.BaseURL)) {
				ch.P.APIKeyEnv = provider.KeyEnvFor(r.BaseURL)
			}
		}
	}
	if o.KeyEnv != "" {
		ch.P.APIKeyEnv = o.KeyEnv
	} else if ch.P.APIKeyEnv == "" && o.Key != "" {
		ch.P.APIKeyEnv = provider.KeyEnvFor(ch.P.BaseURL)
	}
	switch {
	case o.Model != "":
		ch.P.Model = o.Model
	case custom && len(models) == 1:
		ch.P.Model = models[0]
		w.say("using the only model it serves: %s", models[0])
	case custom:
		if len(models) > 0 {
			if len(models) > 12 {
				models = append(models[:12:12], "...")
			}
			return ch, fmt.Errorf("which model? add --model ID. it serves: %s", strings.Join(models, ", "))
		}
		return ch, fmt.Errorf("which model? add --model ID (%s did not list its models; the provider's docs have the id)", ch.P.BaseURL)
	}
	if o.Protocol != "" {
		if o.Protocol != "chat" && o.Protocol != "responses" {
			return ch, fmt.Errorf("--protocol is chat or responses, not %q", o.Protocol)
		}
		ch.P.Protocol = o.Protocol
	}
	if o.Vision {
		ch.P.Vision = true
	}
	for _, h := range o.Headers {
		k, v, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(k) == "" {
			return ch, fmt.Errorf("--header wants \"Name: value\", got %q", h)
		}
		if ch.P.Headers == nil {
			ch.P.Headers = map[string]string{}
		}
		ch.P.Headers[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return ch, nil
}

// cmdProviders prints every preset so a script or a person can pick one
// without opening the docs, plus whatever is already running here.
func cmdProviders() {
	for _, p := range provider.Presets {
		fmt.Printf("%-16s %s\n", p.ID, p.Label)
		fmt.Printf("  cost    %s\n  model   %s\n", p.Cost, p.Model)
		switch {
		case p.KeyEnv == "":
			fmt.Println("  key     none needed")
		default:
			fmt.Printf("  key     %s  (get one: %s)\n", p.KeyEnv, p.KeyURL)
		}
		if p.Note != "" {
			fmt.Printf("  note    %s\n", p.Note)
		}
		fmt.Println()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if found := provider.DetectLocal(ctx); len(found) > 0 {
		fmt.Println("running on this machine right now:")
		for _, l := range found {
			fmt.Printf("  %-22s %s  (%s)\n", l.Name, l.BaseURL, plural(len(l.Models), "model"))
		}
		fmt.Println()
	}
	fmt.Println("use a preset:        mak1zu init --provider <id>")
	fmt.Println("use your own URL:    mak1zu init --base-url https://host/v1 [--model ID] [--key-env VAR]   (or run `mak1zu init` and pick the last entry)")
}
