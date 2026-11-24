# Changelog

Short on purpose: what changed for the person using her, one line each, newest first. A change you can see gets its line in the same commit; the release notes are this file, copied.

## Unreleased

- **One person, everywhere.** Discord and the terminal are the same person after `/link`; `/callme` sets the name she uses for you; owner is a role, not a Discord ID.
- **A ledger instead of a blob.** She remembers where she learned things, what is still open in your life, running jokes that rest between callbacks, and how you want to be treated. `/memories` shows all of it, `/forget` deletes it.
- **Night shift** (opt-in). Once a day she tidies what she knows, writes a private diary (`/diary`) and picks what to bring up. `mak1zu night --dry-run` shows it first.
- **She can write first** (opt-in): quiet hours, a daily cap, back-off when ignored, and "stop" is honoured.
- **Terminal chat.** Plain `mak1zu` opens it, attached to a running `mak1zu run` or on its own; on a fresh machine it asks the setup questions.
- **Panel.** New Chat and People tabs, an ink-on-paper redesign, and the live feed shows what went into each reply.
- **Workshop.** Ask her to change her persona, a skill, a rule or a dial: she shows the diff and applies it only after your yes in a later message.
- **A voice you can measure.** `mak1zu eval --gate` fails when she drifts; `mak1zu persona distill` learns a character from a chat export; `persona pack` / `install` share characters as folders.
- **Mood is per person.** One rude evening is not worn by everyone.
- **Any OpenAI-compatible endpoint** in `init`, with a model picker and a live check; local servers are detected.
- **Fixes.** Bridged webhooks count as people; a reply to a slow request no longer dies with the request; when a model refuses the configured protocol or a stale `reasoning_effort`, Mak1zu retries without it instead of failing (and `doctor` names the real cause instead of blaming the model); Discord slash commands register reliably (shortened `/link` description within Discord's 100-char limit, logged and retried registration errors, UTF-8 safe ephemeral truncation); `FireReminders` passes through `guard.Clean`; first-run chat after `init` loads `.makizu/.env`.

## First public state

Engine, Discord, memory, providers with presets, web panel, SDK, the `.makizu/` rules folder.
