<p align="center"><img src="brand/png/banner-eclipse.png" alt="Mak1zu" width="100%"></p>

<p align="center"><b>The companion you shape.</b><br>
A Go engine for a Discord-native AI companion with memory, a real personality, a web control panel and an SDK. Linux first, any distro, one static binary.</p>

---

Mak1zu comes from a private companion I built and ran for months in a Discord community, talking with real people every day. This is the first time any of it is public. Everything that made her feel like a *person* (short replies, opinions, callbacks, bad days, remembering you, not sounding like customer support) is encoded here as measured rules and tested code, so you can give yours **any** personality on top of a solid base. See [docs/VOICE.md](docs/VOICE.md).

## What you get

- **Servers and DMs**, with per-channel rules: home channels where she talks freely, mention-only rooms where she only wakes on a real `@mention`, owner-only DMs, bounded banter with sister bots.
- **Memory that stays private.** SQLite + full-text search, per-person relationships (familiarity, dynamic, inside jokes), reminders, "forget me". One person's memory never reaches another's prompt. Tested.
- **A voice, not a chatbot.** Shared human-writing substrate + a small character file. Output guard removes leaks, protocol, tics (no more 164 `*stares*` in a row), loops and customer-support tells.
- **Any model.** OpenAI-compatible `chat` and `responses` protocols, ordered fallbacks, circuit breaker, vision routing, reasoning headroom. Local (Ollama, llama.cpp) or hosted.
- **Web panel** on `127.0.0.1:8787`: edit models, routing, behavior, channels and the persona file; changes apply live; test a provider with a real call; secrets are write-only.
- **SDK**: tools and hooks in ~20 lines, transports in 5 methods, personas in Markdown, and any MCP server as a tool source. [docs/SDK.md](docs/SDK.md)
- **Reaction GIFs** from a fixed list of feelings (hug, bonk, smug, facepalm...), never a link she makes up.
- **Slash commands**: `/persona` (owner), `/mood`, `/remember`, `/memories`, `/forget`, `/ping`.
- **Voice eval**: `mak1zu eval examples/eval-inputs.txt` scores a persona on length, tics and robotic tells.
- **Honest failures**: one useful line in the person's language, a silent retry for transient errors, and a promise audit that catches "done, attached!" when nothing was attached.

## Quick start

```bash
go install github.com/snowarch/mak1zu/cmd/mak1zu@latest   # or: make build
mak1zu init ~/mak1zu && cd ~/mak1zu
$EDITOR .env            # MAK1ZU_API_KEY=...  (and MAK1ZU_DISCORD_TOKEN=... for Discord)
set -a; . ./.env; set +a
mak1zu doctor           # checks config, keys, persona, memory
mak1zu chat             # talk to her in the terminal first
mak1zu run              # Discord + panel at http://127.0.0.1:8787
mak1zu service          # prints a hardened systemd user unit
```

Local model, no key: add a provider in the panel (preset "Ollama (local)"), route `text` to it.

Discord setup: create a bot, enable **Message Content Intent**, invite it, set `discord.enabled`, `discord.owner_id`, and your channel IDs (as text) in the panel.

## Make her yours: the `.makizu/` folder

`mak1zu init` creates a `.makizu/` folder (like a `.git`, found by walking up from where you run it). It *is* her: plain Markdown you edit by hand or in the panel, re-read on every message, no restart needed.

```
.makizu/
  personas/maki/persona.md     who she is (pick the active one in the panel)
  rules/10-care.md             house rules, always in her prompt
  servers/<server-id>.md       rules for one server
  channels/<channel-id>.md     rules for one channel
  skills/anime-recs/SKILL.md   know-how she reads on demand (description + body + references/)
  config.json  .env  data/     settings, secrets, memory (never committed)
```

Add a skill by creating a folder with a `SKILL.md`; add a character by copying [docs/PERSONA_TEMPLATE.md](docs/PERSONA_TEMPLATE.md). Rules and skills outrank her habits and moods, never her hard lines. Details in [.makizu/README.md](.makizu/README.md).

## Docs

[Architecture](docs/ARCHITECTURE.md) · [SDK](docs/SDK.md) · [Voice study](docs/VOICE.md) · [Research](docs/RESEARCH.md) · [Security](docs/SECURITY.md) · [Roadmap](ROADMAP.md) · [Brand](brand/README.md)

## Develop

```bash
make test        # go test -race ./...  (offline, ~2 s)
make build       # static, CGO-free binary in bin/
```

Apache-2.0, with a [NOTICE](NOTICE): if you redistribute or modify Mak1zu, keep the notice and credit "Based on Mak1zu by snowarch". The name and artwork in `brand/` are not covered by the license; see [brand/README.md](brand/README.md).
