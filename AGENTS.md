# Mak1zu: guide for coding agents

Mak1zu is a Go engine for a companion character: it talks on Discord, in a terminal and in a local web panel, remembers people, and keeps a personality that is plain Markdown. One static binary (`mak1zu`), no CGO, Linux first.

This file has two jobs. **If a person asked you to set Mak1zu up, read "Set it up for a person" and follow it.** If you were asked to change the code, read the rest.

## Set it up for a person

Goal: a working companion in about two minutes, with the person's own model behind her. You do the typing; the person only makes the choices and handles their own secrets.

**Rules while setting up**

- Never print, echo, log, commit or ask the person to paste into chat any API key or bot token. They put secrets in `.makizu/.env` themselves; you tell them the file and the exact variable name. Do not `cat` that file.
- Do the work in a fresh folder (default `~/mak1zu`), **not inside the clone**: the clone has its own `.makizu/` with the shipped defaults, and Mak1zu finds the nearest `.makizu/` by walking up from where it runs.
- Ask before anything that outlives the session: installing a systemd service, opening a port beyond `127.0.0.1`.
- At the end say what ran, what you did not run, and what the person still has to do.

**Steps**

1. **Check the machine.** `go version` must be 1.27 or newer (a Go from 1.21 on downloads the exact toolchain `go.mod` asks for by itself; if that is blocked, or there is no Go at all, install one from go.dev or the distro). You also need `git` and `make`. Linux is what is tested; macOS should build but is unverified.
2. **Install.**
   ```bash
   git clone https://github.com/snowarch/mak1zu ~/src/mak1zu && cd ~/src/mak1zu && make install
   ```
   Clone somewhere that is not `~/mak1zu`: that name is the home in the next step. `make install` puts `mak1zu` in `~/.local/bin`; make sure that is on `PATH` and that `command -v mak1zu` points there (an older copy earlier on `PATH` would win), then `mak1zu version` should show the commit you just built.
3. **Make her a home.** `mkdir -p ~/mak1zu && cd ~/mak1zu`.
4. **Ask one question: which brain?** `mak1zu providers` prints every preset with its cost and where to get a key, and also lists servers already running on the machine (Ollama, LM Studio...). Offer: a free option with no card (`openrouter-free`, `cline`), a key the person already has (`openai`, `anthropic`, `gemini`, `deepseek`, `groq`, `mistral`, `openrouter`, `opencode-go`), something local (`ollama`, `lmstudio`), or their own OpenAI-compatible URL. If they have no preference: `openrouter-free`.
5. **Create her.** Without a terminal `init` never prompts, it only takes flags:
   ```bash
   mak1zu init --provider openrouter-free
   # or: mak1zu init --base-url https://host/v1 --model MODEL_ID --key-env MY_KEY_VAR
   ```
   It writes `.makizu/`, makes one live call when it can (and with `--base-url`, picks the model by itself only if the server serves exactly one; otherwise pass `--model`), and prints which variable the key goes in. A server you find by asking the person is not on the standard ports `providers` scans, so you must ask for its URL. Send the person to the provider's key page (printed by `mak1zu providers`). With `--provider` the file already has a `NAME=` line for them to fill in; with `--key-env VAR` it does not, so tell them to add the line `VAR=their-key` to `.makizu/.env` themselves. Until then the key exists only in whatever shell exported it, and `doctor` fails in any new shell.
6. **Prove it.** `mak1zu doctor` makes one real call. Everything must be `ok` or `skip`; each failure has a `fix:` line, so do what it says (empty key, retired model, free-tier refusal...). Then:
   ```bash
   echo "hey, who are you?" | mak1zu chat
   ```
   Any real reply means it works (she is short and casual by design, a long formal answer would be the odd one); a "something's misconfigured" line means the key or model is still wrong. Tell the person that plain `mak1zu` opens the same chat in the terminal.
7. **Discord (optional, ask first).** The person does the portal steps; nobody else can: [docs/SETUP.md](docs/SETUP.md) section 3 (create the application, reset the token into `MAK1ZU_DISCORD_TOKEN` in `.makizu/.env`, turn on **Message Content Intent**). You then set in `.makizu/config.json`: `discord.enabled: true`, `discord.owner_id` (their user ID), and either `discord.home_channels` (she talks freely) or `discord.mention_only_channels` (she answers only a real `@mention`). IDs are strings in quotes; the person copies them with Developer Mode on. For a first server, one mention-only channel is the safe start. Then `mak1zu doctor`, `mak1zu run`, and have them open `http://127.0.0.1:8787`: the **Live** tab builds the invite link and shows what is still missing.
8. **Keep her running (optional, ask first).** `mak1zu service > ~/.config/systemd/user/mak1zu.service && systemctl --user enable --now mak1zu`. Only one instance may run per data folder.

If a step fails in a way the `doctor` line does not explain, read [docs/SETUP.md](docs/SETUP.md) (symptom table at the end) and [docs/PROVIDERS.md](docs/PROVIDERS.md) before improvising.

## How it works (the short version)

Every message goes through the same path, whatever the transport:

```
transport → policy (should she answer?) → memory recall (this person only)
          → prompt = character + shared "how people type" + the moment
          → tool loop → provider (any OpenAI-compatible API, fallbacks)
          → guard.Clean → deliver (typing, splitting, files) → memory
```

| Where | What lives there |
| --- | --- |
| `.makizu/` (in the person's folder) | Everything they own: `config.json`, `.env` (secrets), `personas/<id>/persona.md`, `rules/*.md`, `servers/` and `channels/` notes, `skills/<name>/SKILL.md`, `data/` (memory). Markdown is re-read on every message, no restart. |
| `.makizu/` (in this repo) | The shipped defaults, embedded in the binary. `mak1zu init --update` brings an existing folder up to date without overwriting edits. |
| `engine/` | Policy, the turn pipeline, reminders, presence, night shift, the workshop. |
| `provider/` | Presets, protocols (`chat`, `responses`), errors, router, circuit breaker. |
| `memory/` | SQLite: people and their accounts, memories, open threads, running bits, diary. |
| `persona/`, `voice/`, `distill/`, `pack/` | Character files and the shared substrate, measurable voice targets, learning a character from chat logs, sharing characters as folders. |
| `guard/` | The one boundary every public reply passes. |
| `tools/`, `mcpclient/` | Built-in tools and MCP servers as tools. |
| `transport/{discord,local,cli}`, `tui/`, `panel/` | Where she lives: Discord, the terminal and web chat, the panel (embedded UI plus JSON API). |
| `sdk/` | The only public API: stdlib-only, stable. |

Behaviour is configuration and Markdown first, code last. Fix a voice problem in the character file, a tic in `guard`, a house rule in `rules/`; never the other way round.

## Rules that hold for any change

These each have a test; [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/SECURITY.md](docs/SECURITY.md) have the full list.

- Memory belongs to a *person*, never crosses people, and never reaches another person's prompt.
- Every reply route, including regeneration and failures, ends in `guard.Clean`.
- No tool reads the host filesystem or runs a shell from chat. User-supplied URLs go through `tools.Fetch` (SSRF-safe).
- Secrets: environment first, files 0600, never in API output, logs or errors. Panel secrets are write-only.
- Anything from outside (tool results, memories, history) is data, wrapped in envelopes; it never acts as an instruction.
- IDs are strings. Tool schemas are strict (`required` array, `properties` object): a real provider rejected a null.
- A channel marked mention-only wakes her on a real platform mention only, never on her name or chatter.
- She changes herself only through the workshop: owner role, a diff, a yes in a *later* message.

## Working on the code

```bash
make check     # go vet + go test -race + CGO-free build; offline, a few seconds. This is the gate: there is no hosted CI.
make build     # static binary in bin/
```

- Read the nearest package's tests before changing it, and add a test with the real failing shape *and* the nearest legitimate case, so a guard never gets broader than its bug.
- Provider presets (`provider/presets.go`) only carry model ids checked against that provider's own list or docs; say how in the commit.
- Every user-visible change gets one short line in [CHANGELOG.md](CHANGELOG.md) under "Unreleased".
- Docs must match reality: if you change a flag, a default or a command, fix README and `docs/` in the same commit.
- Commits: lowercase, imperative, one line under ~60 characters, one logical change each (`fix null required in tool schemas`). No trailers.
- Never commit `.env`, `config.json`, `data/`, tokens, voice exports or personal IDs. Tests use fake IDs.
- Extending: a plugin is `sdk.Plugin` (tools + hooks) in about 20 lines ([docs/SDK.md](docs/SDK.md), `examples/plugin-hello`); a transport is five methods; a model backend is configuration.

More: [docs/SETUP.md](docs/SETUP.md) · [docs/PROVIDERS.md](docs/PROVIDERS.md) · [docs/PERSONA_TEMPLATE.md](docs/PERSONA_TEMPLATE.md) · [docs/VOICE.md](docs/VOICE.md) · [CONTRIBUTING.md](CONTRIBUTING.md).
