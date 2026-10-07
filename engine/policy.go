package engine

import (
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/snowarch/mak1zu/config"
	"github.com/snowarch/mak1zu/sdk"
)

// Why a message did (or did not) wake the companion.
type Reason string

const (
	ReasonDM          Reason = "dm"
	ReasonMention     Reason = "mention"
	ReasonReply       Reason = "reply_to_her"
	ReasonName        Reason = "name"
	ReasonTask        Reason = "task"
	ReasonAmbient     Reason = "ambient"
	ReasonPeer        Reason = "peer"
	ReasonCeiling     Reason = "rate_ceiling"
	ReasonMentionOnly Reason = "mention_only_channel"
	ReasonPaused      Reason = "paused"
	ReasonOtherServer Reason = "other_server"
	ReasonBotIgnored  Reason = "bot_ignored"
	ReasonPeerLimit   Reason = "peer_limit"
	ReasonOwnerOnly   Reason = "owner_only_dm"
	ReasonNotHome     Reason = "not_home"
	ReasonCooldown    Reason = "cooldown"
	ReasonDice        Reason = "dice"
)

// Human says why, in a sentence the panel can show as is.
func (r Reason) Human() string {
	switch r {
	case ReasonDM:
		return "private message from her owner"
	case ReasonMention:
		return "she was @mentioned"
	case ReasonReply:
		return "someone replied to her message"
	case ReasonName:
		return "someone said her name"
	case ReasonTask:
		return "someone asked for a reminder or a search"
	case ReasonAmbient:
		return "nobody called her. she jumped in anyway"
	case ReasonPeer:
		return "a bot friend spoke in her home channel"
	case ReasonCeiling:
		return "hit her per-minute cap. out of words for now"
	case ReasonMentionOnly:
		return "mention-only room, no @. she stays out of it"
	case ReasonPaused:
		return "paused. she heard it and kept her mouth shut"
	case ReasonOtherServer:
		return "not her server. none of her business"
	case ReasonBotIgnored:
		return "a bot she does not talk to. ignored"
	case ReasonPeerLimit:
		return "enough bot banter. she is done"
	case ReasonOwnerOnly:
		return "DM from a stranger. not answering"
	case ReasonNotHome:
		return "not her room and nobody called her. stayed out"
	case ReasonCooldown:
		return "just spoke. letting it breathe"
	case ReasonDice:
		return "heard it. said nothing"
	}
	return string(r)
}

// Spoke reports whether the reason is one that leads to a reply.
func (r Reason) Spoke() bool {
	switch r {
	case ReasonDM, ReasonMention, ReasonReply, ReasonName, ReasonTask, ReasonAmbient, ReasonPeer:
		return true
	}
	return false
}

// Direct reasons are "being called": they bypass ambient cooldowns and
// outrank burst coalescing.
func (r Reason) Direct() bool {
	switch r {
	case ReasonDM, ReasonMention, ReasonReply, ReasonName, ReasonTask:
		return true
	}
	return false
}

// Policy decides whether to speak. It is pure apart from its own counters, so
// every rule is unit-testable with a fixed random source.
type Policy struct {
	mu        sync.Mutex
	rnd       func() float64
	now       func() time.Time
	lastReply map[string]time.Time
	lastPeer  map[string]time.Time
	peerCount map[string]int
	recent    []time.Time
}

func NewPolicy() *Policy {
	return &Policy{rnd: rand.Float64, now: time.Now, lastReply: map[string]time.Time{}, lastPeer: map[string]time.Time{}, peerCount: map[string]int{}}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

var taskRe = regexp.MustCompile(`(?i)\b(remind me|recuérdame|recuerdame|recordame|set a reminder|search (for|the web)|busca en (la )?web|google it)\b`)

// nameRe is built per call from the persona name; \b keeps "makizu's" but not "makizumaki".
func mentionsName(content, name string) bool {
	if name == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)(^|[^\p{L}\p{N}])` + regexp.QuoteMeta(name) + `($|[^\p{L}\p{N}])`)
	return err == nil && re.MatchString(content)
}

// Who is what the engine knows about the speaker's standing: whether anyone
// holds the owner role and whether this speaker does. Owner is a role on a
// person, so it holds from every transport that person is linked to.
type Who struct{ HasOwner, IsOwner bool }

// Decide returns whether to respond and the reason. self is the companion's
// display name used for name-calls. The owner is taken from discord.owner_id;
// the engine itself uses DecideAs with the owner role.
func (p *Policy) Decide(m sdk.Message, cfg config.Config, self string) (bool, Reason) {
	o := cfg.Discord.OwnerID
	return p.DecideAs(m, cfg, self, Who{HasOwner: o != "", IsOwner: o != "" && m.AuthorID == o})
}

// DecideAs is Decide with the speaker's standing already resolved.
func (p *Policy) DecideAs(m sdk.Message, cfg config.Config, self string, who Who) (bool, Reason) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	d, r := cfg.Discord, cfg.Behavior.Response
	if cfg.Behavior.Paused {
		return false, ReasonPaused
	}
	if m.WebhookID != "" && contains(d.HumanWebhooks, m.WebhookID) {
		m.IsBot = false // a bridged or scripted human
	}

	// Absolute ceiling, enforced even for direct calls: protection against
	// runaway cost in a busy room.
	keep := p.recent[:0]
	for _, t := range p.recent {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	p.recent = keep
	if r.MaxPerMinute > 0 && len(p.recent) >= r.MaxPerMinute {
		return false, ReasonCeiling
	}
	roll := func(prob float64) bool { return prob >= 1 || p.rnd() < prob }
	accept := func(ok bool, why Reason) (bool, Reason) {
		if ok {
			p.recent = append(p.recent, now)
			p.lastReply[m.ChannelID] = now
			return true, why
		}
		return false, ReasonDice
	}

	if len(d.OnlyGuilds) > 0 && m.GuildID != "" && !contains(d.OnlyGuilds, m.GuildID) {
		return false, ReasonOtherServer
	}

	home := contains(d.HomeChannels, m.ChannelID)

	// Peer bots: ambient banter only in home channels, bounded.
	if m.IsBot {
		if !contains(d.PeerBots, m.AuthorID) || !home || contains(d.MentionOnly, m.ChannelID) {
			return false, ReasonBotIgnored
		}
		if r.MaxPeerExchanges > 0 && p.peerCount[m.ChannelID] >= r.MaxPeerExchanges {
			return false, ReasonPeerLimit
		}
		if t, ok := p.lastPeer[m.ChannelID]; ok && now.Sub(t).Seconds() < r.PeerCooldownSecs {
			return false, ReasonPeerLimit
		}
		ok, why := accept(roll(r.Chances.Peer), ReasonPeer)
		if ok {
			p.peerCount[m.ChannelID]++
			p.lastPeer[m.ChannelID] = now
		}
		return ok, why
	}
	// A human speaking resets the peer-exchange budget.
	delete(p.peerCount, m.ChannelID)

	// DMs: only the owner, unless no owner is configured (single-user install).
	if m.IsDM {
		if who.HasOwner && !who.IsOwner {
			return false, ReasonOwnerOnly
		}
		return accept(true, ReasonDM)
	}

	// Mention-only channels: a real platform mention wakes her; the name,
	// replies and ambient chatter never do.
	if contains(d.MentionOnly, m.ChannelID) {
		if m.Mentioned {
			return accept(roll(r.Chances.Mentioned), ReasonMention)
		}
		return false, ReasonMentionOnly
	}

	if m.Mentioned {
		return accept(roll(r.Chances.Mentioned), ReasonMention)
	}
	if m.ReplyToBot {
		return accept(roll(r.Chances.ReplyToHer), ReasonReply)
	}
	if taskRe.MatchString(m.Content) && (home || len(d.Allowed) == 0 || contains(d.Allowed, m.ChannelID)) {
		return accept(true, ReasonTask)
	}
	if mentionsName(m.Content, self) {
		return accept(roll(r.Chances.NameInMessage), ReasonName)
	}
	if len(d.Allowed) > 0 && !contains(d.Allowed, m.ChannelID) && !home {
		return false, ReasonNotHome
	}
	if !home {
		return false, ReasonNotHome // never free-talk outside home channels
	}
	if r.HomeAlwaysReply {
		return accept(true, ReasonAmbient)
	}
	if t, ok := p.lastReply[m.ChannelID]; ok && now.Sub(t).Seconds() < r.AmbientCooldown {
		return false, ReasonCooldown
	}
	active := false
	if t, ok := p.lastReply[m.ChannelID]; ok && now.Sub(t).Seconds() < r.RecentActiveSecs {
		active = true
	}
	question := strings.Contains(m.Content, "?") || strings.Contains(m.Content, "¿")
	switch {
	case question:
		return accept(roll(r.Chances.Question), ReasonAmbient)
	case active:
		return accept(roll(r.Chances.ActiveConvo), ReasonAmbient)
	default:
		prob := r.Chances.Interesting
		if len(strings.TrimSpace(m.Content)) < 12 {
			prob = min(prob, 0.15) // reactions and acks are rarely worth a model call
		}
		return accept(roll(prob), ReasonAmbient)
	}
}
