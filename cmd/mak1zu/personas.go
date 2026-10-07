package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/distill"
	"github.com/snowarch/mak1zu/home"
	"github.com/snowarch/mak1zu/pack"
	"github.com/snowarch/mak1zu/persona"
	"github.com/snowarch/mak1zu/provider"
)

// ask says yes or no: with arrow keys on a terminal, else only if --yes was given.
func ask(yes bool, summary, question string) bool {
	fmt.Println(summary)
	if yes {
		return true
	}
	if isTTY(os.Stdin) && isTTY(os.Stdout) {
		ok, err := huhUI{}.confirm(question)
		return err == nil && ok
	}
	fmt.Println("not a terminal and no --yes: nothing done")
	return false
}

func cmdDistill(st *config.Store, lib persona.Library, args []string) error {
	fs := flag.NewFlagSet("persona distill", flag.ContinueOnError)
	as := fs.String("as", "", "whose messages to learn (default: whoever wrote the most)")
	id := fs.String("id", "", "persona id (default: made from the name)")
	rounds := fs.Int("rounds", 2, "how many times to test and rewrite against the measured targets (1-4)")
	yes := fs.Bool("yes", false, "do not ask before sending the sample to the model")
	force := fs.Bool("force", false, "replace a persona with the same id")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: mak1zu persona distill [flags] EXPORT

Learns a character from somebody's real messages. EXPORT is a Discord export
(DiscordChatExporter JSON or a Discord data package), a WhatsApp .txt, or a text
file of "Name: message" lines. It measures how that person writes, has your
model draft a character from a sample, then tests the character and rewrites it
until it sounds like them. The export stays on this machine; about 40 of the
messages, with links, mentions, emails and numbers removed, go to your provider
after you say yes.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("give me one export file")
	}
	msgs, err := distill.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	router := provider.NewRouter(st.Get)
	res, err := distill.Run(context.Background(), router, distill.Options{
		Msgs: msgs, As: *as, ID: *id, Dir: lib.Dir, Rounds: *rounds, Force: *force,
		MaxTok:  st.Get().Behavior.Turn.MaxTokens,
		Confirm: func(s string) bool { return ask(*yes, s, "send the sample to the model?") },
		Say:     func(f string, a ...any) { fmt.Printf("  "+f+"\n", a...) },
	})
	if err != nil {
		return err
	}
	fmt.Printf("\nwrote %s from %d of %s's messages\n", res.ID, res.Messages, res.Speaker)
	fmt.Printf("  they write: median %d words, %.0f%% lowercase, %.0f%% questions\n", res.Stats.MedianWords, res.Stats.LowerPct, res.Stats.QuestionPct)
	fmt.Printf("  the character: median %d words, %.0f%% lowercase, %.0f%% questions\n", res.Final.MedianWords, res.Final.LowerPct, res.Final.QuestionPct)
	if len(res.Fails) == 0 {
		fmt.Println("  inside every target")
	}
	for _, f := range res.Fails {
		fmt.Println("  still outside:", f)
	}
	for _, f := range res.Files {
		fmt.Println("  ", f)
	}
	fmt.Printf("\nread persona.md and edit what is wrong, then:\n  mak1zu persona use %s\n  mak1zu eval --gate\n", res.ID)
	return nil
}

func cmdPack(st *config.Store, lib persona.Library, args []string) error {
	fs := flag.NewFlagSet("persona pack", flag.ContinueOnError)
	skills := fs.String("skills", "", "comma-separated skills to include")
	rules := fs.String("rules", "", "comma-separated house rules to include")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: mak1zu persona pack ID [--skills a,b] [--rules x,y] [FILE.tar.gz]")
	}
	id := fs.Arg(0)
	out := id + ".tar.gz"
	if fs.NArg() > 1 {
		out = fs.Arg(1)
	}
	f, err := os.OpenFile(out, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	split := func(s string) []string {
		var l []string
		for _, x := range strings.Split(s, ",") {
			if x = strings.TrimSpace(x); x != "" {
				l = append(l, x)
			}
		}
		return l
	}
	h := home.Home{Dir: st.Abs(".")}
	if err := pack.Export(lib.Dir, h, id, split(*skills), split(*rules), f); err != nil {
		f.Close()
		os.Remove(out)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("wrote %s: anyone can install it with `mak1zu persona install %s`\n", out, out)
	return nil
}

func cmdInstall(st *config.Store, lib persona.Library, args []string) error {
	fs := flag.NewFlagSet("persona install", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "do not ask")
	force := fs.Bool("force", false, "replace what already exists (the old versions go to .makizu/.backup)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: mak1zu persona install [--yes] [--force] FOLDER | FILE.tar.gz | user/repo | https://git-address")
	}
	p, cleanup, err := pack.Fetch(fs.Arg(0))
	if err != nil {
		return err
	}
	defer cleanup()
	h := home.Home{Dir: st.Abs(".")}
	if c := p.Conflicts(lib.Dir, h); len(c) > 0 && !*force {
		return fmt.Errorf("this would overwrite %s; pass --force to replace (the old versions are kept in .makizu/.backup)", strings.Join(c, ", "))
	}
	if !ask(*yes, p.Summary(), "install it?") {
		return errors.New("not installed")
	}
	if err := p.Install(lib.Dir, h, *force); err != nil {
		return err
	}
	fmt.Printf("installed %s. try her without switching: mak1zu persona use %s, then mak1zu eval\n", p.ID, p.ID)
	return nil
}
