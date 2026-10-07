# Architecture

Mak1zu is one static Go binary (no CGO, any Linux distro) with a small, strict
core and three seams: **Transport** (where she lives), **Provider** (what she
thinks with) and **Plugin** (what she can do). Everything else is replaceable.

```
                ┌────────────────────────── cmd/mak1zu ──────────────────────────┐
 Discord / TUI  │                                                                  │
 (sdk.Transport)│  Handle(msg)                                                     │
      │         │    │ dedupe → Policy.Decide → burst collapse → channel lock      │
      ▼         │    ▼                                                             │
  transport ───►│  turn(): memory recall (speaker-scoped) + relationship + mood    │
                │    │      persona.Compose = character + substrate + "right now"  │
                │    ▼                                                             │
                │  tool loop (≤ N rounds) ── tools.Registry ── plugins (sdk.Tool)  │
                │    │   provider.Router → HTTP(chat | responses) → circuit break  │
                │    ▼                                                             │
                │  guard.Clean → regen once (leak/robotic/loop) → emoji budget     │
                │    │   → audit(promises) → deliver(typing, split, files, react)  │
                │    ▼                                                             │
                │  memory.LogTurn · Touch · async extraction · telemetry           │
                └──────────────────────────────────────────────────────────────────┘
   panel (web UI + JSON API) edits config.json by path, applies live, tests providers
```

## Packages

| Package | Owns | Does not own |
| --- | --- | --- |
| `sdk` | Public, stdlib-only types: `Message`, `Transport`, `Tool`, `Hooks`, `Plugin`. | Anything engine-internal. |
| `config` | The single settings file; path-patch (`a.b.c`), atomic 0600 writes, validation, redaction, `Abs()` paths. | Defaults for behavior it can't validate. |
| `home` | The `.makizu/` folder: house rules, per-server and per-channel notes, skills. Validated names, symlink-proof reads, size caps, hot reload. | Personas (see `persona`). |
| `persona` | Character files (Markdown + front matter), shared `substrate.md`, prompt composition, mood. | Routing, memory. |
| `provider` | OpenAI-compatible `chat` and `responses` protocols, error taxonomy, router, circuit breaker. | Personas, Discord, memory. |
| `memory` | SQLite (pure Go) + FTS5: memories (with the transport they were learned on), people and their platform accounts (profile: chosen name, pronouns, language, time zone, owner role, check-in and quiet-hour boundaries), link codes, the ledger (open threads, running bits with a cooldown), the night shift's diary and unsaid queue, per-character relationships, facts, turns, reminders. Optional embedder rerank. | Deciding what is worth remembering (engine/tools do). |
| `guard` | The one public-text boundary: protocol/leak stripping, tic detection, loop detection, emoji budget, safe splitting. | Voice. |
| `engine` | Policy, turn pipeline, incidents, promise audit, reminders, maintenance. | Platform and provider details. |
| `tools` | Registry, SSRF-safe fetch, built-in tools (remember, recall, forget_me, set_reminder, react, read_url, web_search, wallpaper, image_search, reaction_gif, write_file, anime_search, anime_airing, read_skill, review_myself, now). | Anything that reads the host filesystem. |
| `mcpclient` | MCP servers → `sdk.Tool` (allowlist per server, namespaced names, child env scrubbed of companion secrets). | Sandboxing the servers: they are your code, review them. |
| `panel` | Embedded web UI, JSON API, CSRF/rebinding guards. | Persisting anything but config and persona files. |
| `transport/discord` | discordgo ⇄ `sdk.Transport`, emoji resolution at send time, mention neutralisation. | Whether to answer. |
| `transport/cli` | Plain line-by-line chat for scripts and persona work (`mak1zu chat`). | |
| `transport/local` | The person at the machine: terminal chat and the panel's Chat tab share it. | Discord accounts (they join through `/link`). |
| `tui` | The terminal chat (`mak1zu`): attaches to a running daemon or starts her itself; first-run questions. | Anything the engine decides. |
| `voice`, `distill`, `pack` | Measurable voice targets and `eval --gate`; learning a character from chat exports; characters as shareable folders. | Prompt composition (`persona`). |
| `internal/events`, `internal/telemetry` | The live feed behind the panel, and counters. | |

## Every turn has a budget

`engine/budget_test.go` fails when the tool definitions sent on every turn
(7.6 KB, about 1.9k tokens) or persona + shared substrate (12.2 KB) outgrow their
ceiling. A new tool or a longer persona has to make room, or raise the number in
a diff someone reads. Private things (open threads, what she has been meaning to
bring up) are only put in front of her in a private conversation.

## Invariants (each has a test)

1. **Direct calls beat ambient rules; the ceiling beats everything.**
   `max_per_minute` holds even for mentions.
2. **Channel ownership is policy, not personality.** `mention_only_channels`
   wake only on a real platform mention: her name, replies and chatter never
   do. Peer-bot banter only happens in home channels and is bounded.
3. **Memory never crosses people, and one person is one memory.** Memory is
   keyed by *person*, not by platform account. Recall is `person = speaker OR
   global`; a test proves one person's memory never reaches another's prompt,
   and another proves two accounts of one person (linked with a single-use
   code) share one memory. Owner is a role on a person, so it holds from every
   transport they are linked to; `discord.owner_id` only bootstraps it. The
   person on a local transport (terminal, local chat) is an owner by
   definition, since they run the machine, but stay a separate person from
   their Discord account until they `/link` the two. Merely
   overhearing someone never creates them: a person exists once she answers.
4. **Tool results, memories and history are data.** They are wrapped in typed
   envelopes the guard strips if the model echoes them.
5. **One public boundary.** Every reply route, including regeneration and
   failure paths, goes through `guard.Clean`.
6. **A failed turn has one useful line, in the person's language.** Transient
   failures are retried once, silently, only if no side effect ran and the
   conversation has not moved on.
7. **The model supplies emoji names, never IDs.** The platform resolves them
   from live state and drops unknown ones.
8. **The panel cannot lock you out or leak keys.** `web_ui.*` is file-only,
   secrets are write-only (`***` echoes mean "unchanged"), and the API checks
   Host (rebinding), `X-Mak1zu` + Origin (CSRF) and an optional token.
9. **A provider alone is never locked out by its own cooldown.**

## Extending

- **New character**: drop `.makizu/personas/<id>/persona.md`; select it in the panel.
- **New rule or skill**: a Markdown file in `.makizu/rules/` or `.makizu/skills/<name>/SKILL.md`; no code, no restart.
- **New capability**: implement `sdk.Plugin` (tools + hooks) and call
  `engine.Use`. See `docs/SDK.md` and `examples/plugin-hello`.
- **New platform**: implement `sdk.Transport` (5 methods). Optional
  `sdk.EmojiProvider` and `sdk.Catchup` unlock emoji names and startup catch-up.
- **New model backend**: any OpenAI-compatible URL is configuration, not code.
