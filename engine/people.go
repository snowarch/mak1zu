package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/snowarch/mak1zu/memory"
	"github.com/snowarch/mak1zu/sdk"
)

// People: one human is one person, whichever transport they arrive on. Memory,
// relationships and reminders hang off the person; an account is only a way to
// reach them. Owner is a role on a person.

func (e *Engine) isLocal() bool {
	l, ok := e.Tr.(sdk.Local)
	return ok && l.Local()
}

// isPeer reports whether the speaker is a bot rather than a person. A webhook
// listed in discord.human_webhooks is a bridged or scripted human, so it is a
// person (the policy already treats it as one).
func (e *Engine) isPeer(m sdk.Message) bool {
	if !m.IsBot {
		return false
	}
	return !(m.WebhookID != "" && contains(e.Cfg.Get().Discord.HumanWebhooks, m.WebhookID))
}

// synthetic is the stand-in for speakers who are not people: peer bots.
func synthetic(m sdk.Message) memory.Person {
	return memory.Person{ID: m.AuthorID, Name: m.AuthorName}
}

// lookupPerson finds who is speaking without creating anyone.
func (e *Engine) lookupPerson(ctx context.Context, m sdk.Message) (memory.Person, bool) {
	if e.isPeer(m) {
		return synthetic(m), true
	}
	p, ok, err := e.Mem.Lookup(ctx, e.Tr.Name(), m.AuthorID)
	if err != nil {
		e.Log.Warn("person lookup", "err", err)
	}
	return p, ok
}

// standing is what the policy needs to know: does an owner exist, is this speaker them.
// discord.owner_id still works as the bootstrap for a Discord account that has
// not been seen yet, and on a local transport the person at the keyboard is
// the owner when nobody else holds the role.
func (e *Engine) standing(ctx context.Context, m sdk.Message, seen memory.Person) Who {
	if e.isPeer(m) {
		return Who{}
	}
	cfgOwner := e.Cfg.Get().Discord.OwnerID
	isCfg := cfgOwner != "" && e.Tr.Name() == "discord" && m.AuthorID == cfgOwner
	has := e.Mem.HasOwner(ctx) || cfgOwner != ""
	if e.isLocal() && !has {
		return Who{HasOwner: true, IsOwner: true}
	}
	return Who{HasOwner: has, IsOwner: seen.IsOwner() || isCfg}
}

// person resolves the speaker, creating the person on first real contact and
// applying the owner bootstrap.
func (e *Engine) person(ctx context.Context, m sdk.Message) memory.Person {
	if e.isPeer(m) {
		return synthetic(m)
	}
	return e.account(ctx, m.AuthorID, m.AuthorName)
}

// account is person() for callers that have an account but no message
// (slash commands).
func (e *Engine) account(ctx context.Context, external, display string) memory.Person {
	p, err := e.Mem.Resolve(ctx, e.Tr.Name(), external, display)
	if err != nil {
		e.Log.Error("resolve person", "err", err)
		return memory.Person{ID: external, Name: display}
	}
	if p.IsOwner() {
		return p
	}
	claim := false
	if o := e.Cfg.Get().Discord.OwnerID; o != "" && e.Tr.Name() == "discord" && external == o {
		claim = true
	} else if e.isLocal() && !e.Mem.HasOwner(ctx) {
		claim = true
	}
	if claim {
		if err := e.Mem.ClaimOwner(ctx, p.ID); err == nil {
			p.Role = memory.RoleOwner
		}
	}
	return p
}

// profileNotes are the lines about this person that go into her prompt: who
// they are to her, how they asked to be treated, and, until she knows, the
// instruction to ask what they want to be called.
func (e *Engine) profileNotes(per memory.Person, rel memory.Relationship, m sdk.Message) []string {
	var out []string
	if per.IsOwner() {
		out = append(out, "This is your owner, the person who runs you. You always know them.")
	}
	var bits []string
	if per.CallMe != "" {
		bits = append(bits, fmt.Sprintf("they asked to be called %q", per.CallMe))
	}
	if per.Pronouns != "" {
		bits = append(bits, "pronouns "+per.Pronouns)
	}
	if per.Language != "" {
		bits = append(bits, "prefers "+per.Language)
	}
	if per.TZ != "" {
		bits = append(bits, "time zone "+per.TZ)
	}
	if !per.WantsCheckins() {
		bits = append(bits, "asked you not to start conversations")
	}
	if per.Quiet != "" {
		bits = append(bits, "do not message them between "+per.Quiet)
	}
	if len(bits) > 0 {
		out = append(out, "About "+per.Display()+": "+strings.Join(bits, ", ")+".")
	}
	// Ask once, lightly, only where it is not an interrogation: a private
	// chat (or the owner's own machine), never a room full of strangers.
	if per.CallMe == "" && (m.IsDM || e.isLocal()) && rel.Interactions < 6 && !e.isPeer(m) {
		out = append(out, fmt.Sprintf("You do not know what %s wants to be called (you only have their account name). When it fits, ask once, casually, what they would like you to call them; when they answer, save it with set_profile. Do not ask again if they brush it off.", per.Name))
	}
	return out
}
