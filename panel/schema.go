package panel

import (
	"encoding/json"
	"strings"

	"github.com/snowarch/mak1zu/config"
)

// Setting describes one config path for the panel: what to call it, what it
// does in plain words, and how to edit it. A test fails when a config field
// has no entry here, so nothing ships as an unexplained knob.
type Setting struct {
	Path     string   `json:"path"`
	Label    string   `json:"label"`
	Help     string   `json:"help"`
	Group    string   `json:"group"`
	Kind     string   `json:"kind"` // toggle | chance | number | seconds | text | ids | lines | secret
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Step     *float64 `json:"step,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Restart  bool     `json:"restart,omitempty"` // needs a restart to take effect
	Advanced bool     `json:"advanced,omitempty"`
	Default  any      `json:"default"`
}

type Group struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Blurb string `json:"blurb"`
}

// Groups in display order.
var Groups = []Group{
	{"talk", "When she talks", "How eager she is to join in. A direct call (@mention, a reply to her, a DM) always gets an answer. These dials shape everything else."},
	{"rhythm", "How she types", "Small human delays so replies do not snap back like a vending machine."},
	{"length", "Length and cost", "How much she reads, how much she writes, and what that costs per message."},
	{"brain", "Brain", "Model behavior that applies to every provider. Providers themselves live in the Models tab."},
	{"rooms", "Rooms", "Where she lives on Discord and who she answers there. IDs are pasted as text, one per line."},
	{"memory", "Memory", "What she keeps about people. Memory is private per person: she never repeats one person's to another."},
	{"tools", "Tools", "Things she can do besides talk."},
	{"panel", "This panel", "Privacy for what you see here."},
	{"advanced", "Under the hood", "You rarely need these."},
}

func f(v float64) *float64 { return &v }

// Catalog lists every editable setting. Order within a group is display order.
var Catalog = []Setting{
	{Path: "behavior.paused", Group: "talk", Kind: "toggle", Label: "Pause her",
		Help: "Mutes her everywhere, at once. She still hears and remembers, but answers nothing, not even DMs. The big red button."},
	{Path: "behavior.response.chances.mentioned", Group: "talk", Kind: "chance", Label: "When someone @mentions her",
		Help: "How often she answers a direct @mention. Leave it at 100% unless you want her to leave people on read."},
	{Path: "behavior.response.chances.reply_to_her", Group: "talk", Kind: "chance", Label: "When someone replies to her",
		Help: "Someone used Discord's reply on one of her messages."},
	{Path: "behavior.response.chances.name_in_message", Group: "talk", Kind: "chance", Label: "When someone says her name",
		Help: "Her name appears in a message without an @. Not applied in mention-only channels."},
	{Path: "behavior.response.chances.question_home", Group: "talk", Kind: "chance", Label: "A question in a home channel",
		Help: "Nobody called her, but someone asked a question in a channel she treats as home."},
	{Path: "behavior.response.chances.active_convo", Group: "talk", Kind: "chance", Label: "Mid-conversation",
		Help: "She spoke in the last couple of minutes. Chance she joins the next message without being called."},
	{Path: "behavior.response.chances.interesting_home", Group: "talk", Kind: "chance", Label: "Idle chatter in a home channel",
		Help: "Random messages with no question and no history. Keep this low or she turns into a hall monitor."},
	{Path: "behavior.response.chances.peer", Group: "talk", Kind: "chance", Label: "When a bot friend speaks",
		Help: "Chance she answers a bot listed under 'bot friends', in a home channel only."},
	{Path: "behavior.response.home_always_reply", Group: "talk", Kind: "toggle", Label: "Answer everything in home channels",
		Help: "Skips every dice roll above in home channels. Very chatty, and it costs more."},
	{Path: "behavior.response.ambient_cooldown_seconds", Group: "talk", Kind: "seconds", Min: f(0), Max: f(600), Unit: "s", Label: "Breathing room",
		Help: "After she speaks, she will not join in uninvited again for this long."},
	{Path: "behavior.response.recent_active_seconds", Group: "talk", Kind: "seconds", Min: f(0), Max: f(3600), Unit: "s", Label: "Conversation window",
		Help: "How long after she speaks still counts as 'she is in this conversation'."},
	{Path: "behavior.response.burst_settle_seconds", Group: "talk", Kind: "seconds", Min: f(0), Max: f(30), Step: f(0.5), Unit: "s", Label: "Wait for the dust to settle",
		Help: "When many messages land together, she waits this long and only answers the last uninvited one. Direct calls are never skipped."},
	{Path: "behavior.response.max_per_minute", Group: "talk", Kind: "number", Min: f(0), Max: f(120), Label: "Replies per minute, hard cap",
		Help: "A ceiling for the whole bot, even for @mentions. It protects your wallet in a busy room. 0 means no cap."},
	{Path: "behavior.response.peer_cooldown_seconds", Group: "talk", Kind: "seconds", Min: f(0), Max: f(3600), Unit: "s", Label: "Bot banter spacing", Advanced: true,
		Help: "Minimum gap between her lines to a bot friend."},
	{Path: "behavior.response.max_peer_exchanges", Group: "talk", Kind: "number", Min: f(0), Max: f(50), Label: "Bot banter limit", Advanced: true,
		Help: "How many lines she trades with bots before she stops, until a human speaks again. 0 means no limit."},

	{Path: "behavior.timing.think_min", Group: "rhythm", Kind: "seconds", Min: f(0), Max: f(30), Step: f(0.1), Unit: "s", Label: "Shortest thinking pause",
		Help: "She waits a random time between the shortest and longest pause before she starts typing."},
	{Path: "behavior.timing.think_max", Group: "rhythm", Kind: "seconds", Min: f(0), Max: f(30), Step: f(0.1), Unit: "s", Label: "Longest thinking pause",
		Help: "Upper end of the pause above."},
	{Path: "behavior.timing.typing_seconds_per_char", Group: "rhythm", Kind: "seconds", Min: f(0), Max: f(0.5), Step: f(0.005), Unit: "s", Label: "Typing speed",
		Help: "Seconds of 'typing…' per character of the reply. Lower is faster."},
	{Path: "behavior.timing.typing_max_seconds", Group: "rhythm", Kind: "seconds", Min: f(0), Max: f(60), Unit: "s", Label: "Longest typing indicator",
		Help: "Caps the typing indicator so long replies do not leave people staring at dots."},

	{Path: "behavior.turn.max_tokens", Group: "length", Kind: "number", Min: f(40), Max: f(4000), Label: "Normal reply budget",
		Help: "Max tokens for an everyday chat reply. About 240 is a few sentences."},
	{Path: "behavior.turn.heavy_tokens", Group: "length", Kind: "number", Min: f(100), Max: f(8000), Label: "Long answer budget",
		Help: "Used when she is asked to research, summarize or write a file."},
	{Path: "behavior.turn.history_limit", Group: "length", Kind: "number", Min: f(0), Max: f(100), Label: "Messages she reads",
		Help: "How many recent channel messages she sees before answering. More context costs more."},
	{Path: "behavior.turn.max_rounds", Group: "length", Kind: "number", Min: f(1), Max: f(8), Label: "Tool rounds",
		Help: "How many times she may use tools in one turn before she has to answer."},
	{Path: "behavior.turn.tool_turn_budget_seconds", Group: "length", Kind: "seconds", Min: f(5), Max: f(300), Unit: "s", Label: "Tool time limit", Advanced: true,
		Help: "How long tools may run in one turn."},
	{Path: "behavior.turn.max_reply_chars", Group: "length", Kind: "number", Min: f(200), Max: f(1990), Label: "Message size limit", Advanced: true,
		Help: "Discord caps messages at 2000 characters. Longer replies are split at sentence boundaries."},
	{Path: "behavior.turn.emoji_budget", Group: "length", Kind: "number", Min: f(0), Max: f(10), Label: "Emoji per reply",
		Help: "Most custom emoji she may use in one message. 0 turns them off."},

	{Path: "llm.temperature", Group: "brain", Kind: "number", Min: f(0), Max: f(2), Step: f(0.05), Label: "Creativity",
		Help: "Higher is looser and more surprising, lower is steadier. A persona file can override it."},
	{Path: "language", Group: "brain", Kind: "text", Label: "Default language",
		Help: "'auto' means English until someone writes in another language, then she follows them. Set a language name to change the default."},
	{Path: "behavior.proactive", Group: "talk", Kind: "toggle", Label: "Let her start conversations",
		Help: "She may write first in a private chat, to say something the night shift left on her mind. Only to people who have already DMed her, never between 23:00 and 09:00 (or their own quiet hours), at most one a day, less often if they ignore her, never after they tell her to stop. Needs the night shift on."},
	{Path: "behavior.retry_transient", Group: "brain", Kind: "toggle", Label: "Retry once on hiccups",
		Help: "If the provider times out on a direct call, she quietly tries once more before apologizing."},

	{Path: "discord.enabled", Group: "rooms", Kind: "toggle", Restart: true, Label: "Connect to Discord",
		Help: "Needs a bot token in your .env (the variable named under Under the hood). Restart to apply."},
	{Path: "discord.owner_id", Group: "rooms", Kind: "text", Label: "Owner (your user ID)",
		Help: "Only the owner can DM her and use owner-only slash commands. Empty means anyone can DM her. Enable Developer Mode in Discord, right-click yourself, Copy User ID."},
	{Path: "discord.home_channels", Group: "rooms", Kind: "ids", Label: "Home channels",
		Help: "Where she may talk freely and join in uninvited. Everywhere else she only answers when called."},
	{Path: "discord.mention_only_channels", Group: "rooms", Kind: "ids", Label: "Mention-only channels",
		Help: "Only a real @mention wakes her here. Her name, replies and chatter never do. Good for rooms that belong to someone else."},
	{Path: "discord.allowed_channels", Group: "rooms", Kind: "ids", Label: "Allowed channels",
		Help: "If set, she ignores every channel not listed here (and not home). Empty means all channels."},
	{Path: "discord.only_guilds", Group: "rooms", Kind: "ids", Label: "Only these servers",
		Help: "If set, she ignores every other server she has been invited to. Empty means all of them. DMs follow the owner setting instead."},
	{Path: "discord.peer_bots", Group: "rooms", Kind: "ids", Label: "Bot friends",
		Help: "User IDs of bots she may banter with in home channels. Every other bot is ignored."},
	{Path: "discord.human_webhooks", Group: "rooms", Kind: "ids", Label: "Webhooks that are people", Advanced: true,
		Help: "Webhook IDs of chat bridges whose messages count as humans. Any other webhook counts as a bot."},
	{Path: "discord.register_commands", Group: "rooms", Kind: "toggle", Restart: true, Label: "Publish slash commands", Advanced: true,
		Help: "Turn off if another program shares this bot application: publishing replaces that application's global commands."},

	{Path: "memory.recall_limit", Group: "memory", Kind: "number", Min: f(0), Max: f(30), Label: "Memories per reply",
		Help: "How many things she remembers about the speaker are put in front of her each time."},
	{Path: "memory.auto_extract", Group: "memory", Kind: "toggle", Label: "Learn by listening",
		Help: "She notices durable facts people mention ('my cat is called Nube') and saves them. Off means she only remembers what she is told to."},
	{Path: "memory.night_shift", Group: "memory", Kind: "toggle", Label: "Night shift",
		Help: "Once a night she goes over the day with the people she actually talked to: tidies what she knows, closes what ended, writes a short private diary entry (they can read it with /diary) and decides what she'd like to bring up next time. Costs a few model calls a night, so it is off until you turn it on."},
	{Path: "memory.night_hour", Group: "memory", Kind: "number", Min: f(0), Max: f(23), Unit: "h", Label: "Night shift starts at", Advanced: true,
		Help: "Local hour after which the night runs, once a day. 4 means she reflects some time after 04:00."},
	{Path: "memory.night_people", Group: "memory", Kind: "number", Min: f(1), Max: f(20), Label: "People per night", Advanced: true,
		Help: "The most people she reflects on in one night, busiest first. Each is one model call, so this is your cost cap."},
	{Path: "memory.maintenance_hours", Group: "memory", Kind: "number", Min: f(0), Max: f(720), Unit: "h", Label: "Tidy-up interval", Advanced: true,
		Help: "How often she merges and prunes old memories. 0 turns it off."},

	{Path: "tools.disabled", Group: "tools", Kind: "lines", Label: "Switched-off tools",
		Help: "Tool names she must not use, one per line. The Tools tab lists what exists."},
	{Path: "search.searxng_url", Group: "tools", Kind: "text", Label: "Web search server",
		Help: "URL of your SearXNG instance. Empty means she has no web search."},

	{Path: "web_ui.hide_messages", Group: "panel", Kind: "toggle", Label: "Hide what people say",
		Help: "Keeps message text out of the live feed. You still see who she answered and why. Useful when sharing your screen."},

	{Path: "name", Group: "advanced", Kind: "text", Advanced: true, Label: "Install name",
		Help: "Label for this install. The character's own name comes from the persona file."},
	{Path: "persona.dir", Group: "advanced", Kind: "text", Advanced: true, Restart: true, Label: "Personas folder",
		Help: "Where persona files live, relative to the .makizu folder."},
	{Path: "memory.path", Group: "advanced", Kind: "text", Advanced: true, Restart: true, Label: "Memory database",
		Help: "Path of the SQLite file, relative to the .makizu folder."},
	{Path: "discord.token_env", Group: "advanced", Kind: "text", Advanced: true, Restart: true, Label: "Bot token variable",
		Help: "Name of the environment variable (or .env entry) that holds the Discord bot token. The token itself is never shown."},
}

// hiddenPrefixes are config areas with their own dedicated screens or that the
// panel must never edit (its own door).
var hiddenPrefixes = []string{
	"llm.providers",     // Models tab
	"llm.routing",       // Models tab
	"persona.active",    // Her tab
	"tools.mcp_servers", // Tools tab
	"discord.guilds",
	"web_ui.enabled", "web_ui.host", "web_ui.port", "web_ui.token", // editing where its own door is would lock you out
}

func hidden(path string) bool {
	for _, p := range hiddenPrefixes {
		if path == p || strings.HasPrefix(path, p+".") {
			return true
		}
	}
	return false
}

// schemaWithDefaults returns the catalog with each default filled from
// config.Default().
func schemaWithDefaults() []Setting {
	def := configMap(config.Default())
	out := make([]Setting, len(Catalog))
	for i, s := range Catalog {
		s.Default = lookup(def, s.Path)
		out[i] = s
	}
	return out
}

func configMap(c config.Config) map[string]any {
	b, _ := json.Marshal(c)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func lookup(m map[string]any, path string) any {
	var cur any = m
	for _, k := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[k]
	}
	return cur
}

// leafPaths lists every scalar (or null, or array) path in a config map.
func leafPaths(prefix string, m map[string]any, out *[]string) {
	for k, v := range m {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		if sub, ok := v.(map[string]any); ok && len(sub) > 0 {
			leafPaths(p, sub, out)
			continue
		}
		*out = append(*out, p)
	}
}
