package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/provider"
	"github.com/snowarch/mak1zu/sdk"
)

func TestEveryNoHasItsOwnReason(t *testing.T) {
	cfg := config.Default()
	cfg.Discord.HomeChannels = []string{"home"}
	cfg.Discord.OwnerID = "owner"
	cfg.Discord.OnlyGuilds = []string{"g"}
	cfg.Discord.PeerBots = []string{"sis"}
	cases := []struct {
		name string
		m    sdk.Message
		want Reason
	}{
		{"other server", sdk.Message{ChannelID: "x", GuildID: "elsewhere", AuthorID: "u", Content: "hi"}, ReasonOtherServer},
		{"stranger dm", sdk.Message{ChannelID: "d", IsDM: true, AuthorID: "stranger", Content: "hi"}, ReasonOwnerOnly},
		{"random channel", sdk.Message{ChannelID: "other", GuildID: "g", AuthorID: "u", Content: "hello there everyone"}, ReasonNotHome},
		{"stranger bot", sdk.Message{ChannelID: "home", GuildID: "g", AuthorID: "bot2", IsBot: true, Content: "beep"}, ReasonBotIgnored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, why := NewPolicy().Decide(tc.m, cfg, "Maki")
			if ok || why != tc.want {
				t.Fatalf("ok=%v why=%q want %q", ok, why, tc.want)
			}
			if why.Human() == "" || why.Human() == string(why) {
				t.Fatalf("%q has no plain-language description", why)
			}
		})
	}
}

func TestEveryReasonIsDescribed(t *testing.T) {
	for _, r := range []Reason{ReasonDM, ReasonMention, ReasonReply, ReasonName, ReasonTask, ReasonAmbient, ReasonPeer, ReasonCeiling,
		ReasonMentionOnly, ReasonPaused, ReasonOtherServer, ReasonBotIgnored, ReasonPeerLimit, ReasonOwnerOnly, ReasonNotHome, ReasonCooldown, ReasonDice} {
		if r.Human() == string(r) {
			t.Errorf("%q falls through to its id", r)
		}
	}
}

func TestPausedMutesEvenDirectCalls(t *testing.T) {
	cfg := config.Default()
	cfg.Behavior.Paused = true
	p := NewPolicy()
	for _, m := range []sdk.Message{
		{ChannelID: "home", AuthorID: "u", Mentioned: true},
		{ChannelID: "dm", IsDM: true, AuthorID: "u"},
	} {
		if ok, why := p.Decide(m, cfg, "Maki"); ok || why != ReasonPaused {
			t.Fatalf("paused but ok=%v why=%q", ok, why)
		}
	}
}

func TestFeedShowsHeardThenRepliedWithText(t *testing.T) {
	e, _, _ := setup(t, say("ok fine, i'm here"))
	e.Handle(context.Background(), msg("1", "hey"))
	evs := e.Ev.Since(0)
	if len(evs) != 2 || evs[0].Type != "heard" || evs[1].Type != "replied" {
		t.Fatalf("%+v", evs)
	}
	if evs[0].Reason != "mention" || evs[0].Author != "Alice" || evs[0].Text != "hey" || evs[0].Why == "" {
		t.Fatalf("heard: %+v", evs[0])
	}
	if evs[1].Text != "ok fine, i'm here" || evs[1].Words != 4 {
		t.Fatalf("replied: %+v", evs[1])
	}
}

func TestFeedExplainsSilence(t *testing.T) {
	e, tr, _ := setup(t)
	m := msg("1", "anyone here?")
	m.Mentioned, m.ChannelID = false, "elsewhere"
	e.Handle(context.Background(), m)
	evs := e.Ev.Since(0)
	if len(tr.texts()) != 0 || len(evs) != 1 || evs[0].Type != "quiet" || evs[0].Reason != "not_home" {
		t.Fatalf("%+v %v", evs, tr.texts())
	}
}

func TestFeedCanHideWhatPeopleSaid(t *testing.T) {
	e, _, _ := setup(t, say("sure"))
	if err := e.Cfg.Patch(map[string]any{"web_ui.hide_messages": true}); err != nil {
		t.Fatal(err)
	}
	e.Handle(context.Background(), msg("1", "my secret plan"))
	for _, ev := range e.Ev.Since(0) {
		if strings.Contains(ev.Text, "secret") || strings.Contains(ev.Text, "sure") {
			t.Fatalf("text leaked into the feed: %+v", ev)
		}
	}
}

func TestFeedReportsIncidentsInPlainWords(t *testing.T) {
	e, _, _ := setup(t, fail(&provider.Error{Kind: provider.KindAuth, Status: 401}))
	e.Handle(context.Background(), msg("1", "hey"))
	var got string
	for _, ev := range e.Ev.Since(0) {
		if ev.Type == "incident" {
			got = ev.Why
		}
	}
	if !strings.Contains(got, "rejected") {
		t.Fatalf("incident not described: %q", got)
	}
}

func TestPausedEngineSaysNothingButFeedKnowsWhy(t *testing.T) {
	e, tr, _ := setup(t, say("should not be sent"))
	if err := e.Cfg.Patch(map[string]any{"behavior.paused": true}); err != nil {
		t.Fatal(err)
	}
	e.Handle(context.Background(), msg("1", "hey"))
	evs := e.Ev.Since(0)
	if len(tr.texts()) != 0 || len(evs) != 1 || evs[0].Reason != "paused" {
		t.Fatalf("%v %+v", tr.texts(), evs)
	}
}

func TestPreviewUsesLivePersonaAndShowsThePrompt(t *testing.T) {
	e, tr, sc := setup(t, say("hey. what's up"))
	res, err := e.Preview(context.Background(), "Dana", []PreviewTurn{{Role: "you", Text: "hi"}})
	if err != nil || res.Reply != "hey. what's up" {
		t.Fatalf("%v %+v", err, res)
	}
	if !strings.Contains(res.System, "You are Maki.") || !strings.Contains(res.System, "Dana") {
		t.Fatalf("prompt missing persona or speaker:\n%s", res.System)
	}
	if len(tr.texts()) != 0 || len(e.Ev.Since(0)) != 0 {
		t.Fatal("a preview must not touch the platform or the feed")
	}
	if st := e.Mem.Stats(context.Background()); st["turns"] != 0 {
		t.Fatalf("preview wrote memory: %v", st)
	}
	_ = sc
}

func TestPreviewKeepsTheConversationInOrder(t *testing.T) {
	var seen provider.Request
	e, _, _ := setup(t, func(r provider.Request) (provider.Response, error) {
		seen = r
		return provider.Response{Text: "fine"}, nil
	})
	_, err := e.Preview(context.Background(), "Dana", []PreviewTurn{{"you", "a"}, {"her", "b"}, {"you", "c"}})
	if err != nil || len(seen.Messages) != 3 || seen.Messages[1].Role != provider.Assistant || !strings.HasSuffix(seen.Messages[2].Content, "c") {
		t.Fatalf("%v %+v", err, seen.Messages)
	}
	if _, err := e.Preview(context.Background(), "", nil); err == nil {
		t.Fatal("empty conversation should be refused")
	}
}

func TestFeedFlagsFirstContactAndSlips(t *testing.T) {
	e, _, _ := setup(t, say("done, reminder set"), say("i set it"))
	e.Handle(context.Background(), msg("1", "remind me to stretch"))
	var first, slip bool
	for _, ev := range e.Ev.Since(0) {
		if ev.Type == "heard" && ev.First {
			first = true
		}
		if ev.Type == "slip" && ev.Reason == "unmet_promise" {
			slip = true
		}
	}
	if !first || !slip {
		t.Fatalf("first=%v slip=%v %+v", first, slip, e.Ev.Since(0))
	}
	e.Handle(context.Background(), msg("2", "thanks"))
	evs := e.Ev.Since(0)
	for _, ev := range evs[len(evs)-3:] {
		if ev.Type == "heard" && ev.First {
			t.Fatalf("second message from the same person is not a first contact: %+v", ev)
		}
	}
}
