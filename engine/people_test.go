package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

type localTransport struct{ fakeTransport }

func (*localTransport) Name() string { return "cli" }
func (*localTransport) Local() bool  { return true }

func TestStayingQuietCreatesNoOne(t *testing.T) {
	e, _, _ := setup(t)
	m := msg("1", "just chatting, not to her")
	m.Mentioned = false
	m.ChannelID = "elsewhere"
	e.Handle(context.Background(), m)
	if n := e.Mem.Stats(context.Background())["people"]; n != 0 {
		t.Fatalf("a message she ignored created %d people", n)
	}
	if _, ok, _ := e.Mem.Lookup(context.Background(), "discord", "u1"); ok {
		t.Fatal("an ignored account got a person")
	}
}

func TestChosenNameReachesPromptAndSurvivesAccounts(t *testing.T) {
	call := func(provider.Request) (provider.Response, error) {
		return provider.Response{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "set_profile", Args: `{"field":"call_me","value":"Ren"}`}}}, nil
	}
	e, _, sc := setup(t, call, say("ren it is"), say("hi again"))
	dm := msg("1", "call me Ren")
	dm.IsDM, dm.GuildID = true, ""
	e.Handle(context.Background(), dm)
	if !strings.Contains(sc.reqs[0].System, "ask once") {
		t.Fatal("first DM should tell her to ask what to call the person")
	}
	dm2 := msg("2", "hello")
	dm2.IsDM, dm2.GuildID = true, ""
	e.Handle(context.Background(), dm2)
	last := sc.reqs[len(sc.reqs)-1]
	if !strings.Contains(last.System, `"Ren"`) || strings.Contains(last.System, "ask once") {
		t.Fatalf("name not pinned, or still asking:\n%s", last.System)
	}
	if got := last.Messages[len(last.Messages)-1].Content; !strings.Contains(got, "Ren") || strings.Contains(got, "Alice") {
		t.Fatalf("speaker label should be the chosen name: %q", got)
	}
}

func TestNameIsNotAskedInARoomOfStrangers(t *testing.T) {
	e, _, sc := setup(t, say("hi"))
	e.Handle(context.Background(), msg("1", "hey")) // a guild channel
	if strings.Contains(sc.reqs[0].System, "ask once") {
		t.Fatal("she must not interrogate people in a public room")
	}
}

func TestLinkedAccountsShareOneMemoryButNeverWithOthers(t *testing.T) {
	e, _, sc := setup(t, say("one"), say("two"), say("three"))
	ctx := context.Background()
	// alice speaks from account u1 and teaches her something
	e.Handle(ctx, msg("1", "hi"))
	pid := personID(t, e, "u1")
	e.Mem.Remember(ctx, "maki", "semantic", pid, "Alice keeps an axolotl named Pudding", 0.8, "")
	// bob is another person
	b := msg("2", "tell me about the axolotl")
	b.AuthorID, b.AuthorName = "u2", "Bob"
	e.Handle(ctx, b)
	if strings.Contains(sc.reqs[1].System, "Pudding") {
		t.Fatal("alice's memory reached bob")
	}
	// alice joins a second account to herself
	code, err := e.Mem.NewLinkCode(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if out := findCmd(e, "link").Run(ctx, sdk.CommandCall{UserID: "u1-alt", UserName: "alice-alt", Args: map[string]string{"code": code}}); !strings.Contains(out, "linked") {
		t.Fatal(out)
	}
	a2 := msg("3", "what was my axolotl called?")
	a2.AuthorID, a2.AuthorName = "u1-alt", "alice-alt"
	e.Handle(ctx, a2)
	if !strings.Contains(sc.reqs[2].System, "Pudding") {
		t.Fatalf("the linked account did not get alice's memory:\n%s", sc.reqs[2].System)
	}
}

func TestOwnerIsARoleAcrossAccounts(t *testing.T) {
	e, tr, _ := setup(t, say("hey boss"), say("hey boss again"))
	ctx := context.Background()
	e.Cfg.Patch(map[string]any{"discord.owner_id": "owner"})
	dm := func(id, author string) sdk.Message {
		m := msg(id, "hi")
		m.IsDM, m.GuildID, m.AuthorID, m.AuthorName = true, "", author, author
		return m
	}
	e.Handle(ctx, dm("1", "stranger"))
	if len(tr.texts()) != 0 {
		t.Fatal("a stranger's DM was answered while an owner exists")
	}
	e.Handle(ctx, dm("2", "owner"))
	if len(tr.texts()) != 1 {
		t.Fatal("the owner's DM was not answered")
	}
	// the owner links a second account; it is the owner too, with no config change
	oid := personID(t, e, "owner")
	code, _ := e.Mem.NewLinkCode(ctx, oid)
	if out := findCmd(e, "link").Run(ctx, sdk.CommandCall{UserID: "owner-phone", UserName: "phone", Args: map[string]string{"code": code}}); !strings.Contains(out, "linked") {
		t.Fatal(out)
	}
	e.Handle(ctx, dm("3", "owner-phone"))
	if len(tr.texts()) != 2 {
		t.Fatal("the owner's linked account was refused: owner must be a role, not a platform id")
	}
	if out := findCmd(e, "persona").Run(ctx, sdk.CommandCall{UserID: "owner-phone", Args: map[string]string{"id": "ghost"}}); out != "no such persona" {
		t.Fatalf("owner-only command refused the owner's linked account: %q", out)
	}
	if out := findCmd(e, "persona").Run(ctx, sdk.CommandCall{UserID: "stranger", Args: map[string]string{"id": "maki"}}); !strings.Contains(out, "only the owner") {
		t.Fatalf("stranger passed the owner gate: %q", out)
	}
}

func TestPersonAtTheKeyboardIsTheOwner(t *testing.T) {
	e, _, _ := setup(t, say("hi"))
	lt := &localTransport{}
	e.Tr = lt
	e.Handle(context.Background(), sdk.Message{ID: "1", ChannelID: "cli", AuthorID: "user", AuthorName: "snow", Content: "hey", IsDM: true})
	p, ok, _ := e.Mem.Lookup(context.Background(), "cli", "user")
	if !ok || !p.IsOwner() {
		t.Fatalf("the first person on a local transport should own the install: %+v", p)
	}
}

func TestLocalUserIsOwnerButNotTheSamePersonAsDiscordUntilLinked(t *testing.T) {
	e, _, _ := setup(t, say("hi"))
	ctx := context.Background()
	disc, _ := e.Mem.Resolve(ctx, "discord", "boss", "boss")
	e.Mem.ClaimOwner(ctx, disc.ID)
	lt := &localTransport{}
	e.Tr = lt
	e.Handle(ctx, sdk.Message{ID: "1", ChannelID: "cli", AuthorID: "user", AuthorName: "visitor", Content: "hey", IsDM: true})
	if len(lt.texts()) != 1 {
		t.Fatal("the person at the machine was refused")
	}
	p, ok, _ := e.Mem.Lookup(ctx, "cli", "user")
	if !ok || !p.IsOwner() || p.ID == disc.ID {
		t.Fatalf("local person should be an owner and a separate person: %+v", p)
	}
}

func TestADiscordStrangerNeverBecomesOwner(t *testing.T) {
	e, _, _ := setup(t)
	ctx := context.Background()
	disc, _ := e.Mem.Resolve(ctx, "discord", "boss", "boss")
	e.Mem.ClaimOwner(ctx, disc.ID)
	if p := e.account(ctx, "discord", "stranger", "stranger"); p.IsOwner() {
		t.Fatal("a Discord stranger claimed the owner role")
	}
}

func TestBridgedHumanWebhookIsAPersonButARealBotIsNot(t *testing.T) {
	e, _, _ := setup(t, say("hi"), say("hi"))
	ctx := context.Background()
	e.Cfg.Patch(map[string]any{"discord.human_webhooks": []any{"wh1"}})
	h := msg("1", "hello")
	h.IsBot, h.WebhookID, h.AuthorID, h.AuthorName = true, "wh1", "bridge-user", "Bridged"
	e.Handle(ctx, h)
	if _, ok, _ := e.Mem.Lookup(ctx, "discord", "bridge-user"); !ok {
		t.Fatal("a bridged human is not a person")
	}
	b := msg("2", "hello")
	b.IsBot, b.AuthorID, b.AuthorName, b.Mentioned = true, "some-bot", "SomeBot", false
	if p := e.person(ctx, b); p.ID != "some-bot" {
		t.Fatalf("a real bot became a person: %+v", p)
	}
	if _, ok, _ := e.Mem.Lookup(ctx, "discord", "some-bot"); ok {
		t.Fatal("a real bot got an account")
	}
}

func TestOnePersonsBadMoodIsNotWornByEveryoneElse(t *testing.T) {
	steps := make([]func(providerRequest) (providerResponse, error), 20)
	for i := range steps {
		steps[i] = say("ok")
	}
	e, _, sc := setup(t, steps...)
	e.Cfg.Patch(map[string]any{"behavior.response.max_per_minute": 0}) // the ceiling would stop her at ten
	ctx := context.Background()
	rude := "this is awful and broken, i hate this, terrible, so annoying"
	for i := 0; i < 16; i++ {
		e.Handle(ctx, dmFrom("r"+string(rune('a'+i)), "u1", "Alice", rude))
	}
	alice := e.moodFor(personID(t, e, "u1")).Snapshot()
	if alice.Valence > -0.2 {
		t.Fatalf("sixteen rude messages should sour her toward alice, valence %.2f", alice.Valence)
	}
	e.Handle(ctx, dmFrom("b1", "u2", "Bob", "hi, how are you"))
	if bob := e.moodFor(personID(t, e, "u2")).Snapshot(); bob.Valence < -0.05 {
		t.Fatalf("alice's rudeness leaked into how she feels about bob: %.2f", bob.Valence)
	}
	if strings.Contains(sc.reqs[len(sc.reqs)-1].System, "irritated") {
		t.Fatal("bob's prompt says she is irritated")
	}
}
