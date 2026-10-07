# Setup

From nothing to her answering in a Discord server. About ten minutes, most of it clicking around Discord's developer portal.

## 1. Install

You need Go 1.27 or newer. The binary is one static file with no C dependencies; any Linux distro runs it.

```bash
git clone https://github.com/snowarch/mak1zu && cd mak1zu
make install                  # builds and copies mak1zu to ~/.local/bin
```

Or `go install github.com/snowarch/mak1zu/cmd/mak1zu@latest`.

## 2. Make her a home and pick a brain

```bash
mkdir ~/mak1zu && cd ~/mak1zu
mak1zu init                   # asks which provider, takes the key (hidden)
mak1zu doctor                 # one real call; if it fails it says what to fix
mak1zu chat                   # talk to her in the terminal first
```

`init` creates `.makizu/`: her config, personality, rules and skills, plus a `.env` for secrets. Which provider? Free ones, cheap ones and local ones are laid out in [PROVIDERS.md](PROVIDERS.md). For a script: `mak1zu init --provider openrouter-free`.

Test her here before Discord. If she sounds wrong in the terminal she will sound wrong everywhere, and fixing it is a Markdown edit in `.makizu/personas/maki/persona.md`.

## 3. Create the Discord bot

1. Open the [developer portal](https://discord.com/developers/applications), **New Application**, name it whatever you want her called.
2. **Bot** tab → **Reset Token** → copy it. Put it in `.makizu/.env` as `MAK1ZU_DISCORD_TOKEN=...`. Treat it like a password; anyone with it controls the bot.
3. Same tab, **Privileged Gateway Intents** → turn on **Message Content Intent**. Without it she can see that messages exist but not read them.
4. In `.makizu/config.json` set `"discord": { "enabled": true, ... }`, or flip it in the panel (step 5).

## 4. Tell her who you are and where she lives

Discord IDs are long numbers. Turn on **Developer Mode** (User Settings → Advanced), then right-click a user, channel or server → **Copy ID**. Put them in as text, in quotes.

| Setting | What it does |
| --- | --- |
| `discord.owner_id` | You. With it set, she answers DMs only from you, and only you can use `/persona`. Leave it empty and anyone can DM her. |
| `discord.home_channels` | Channels where she talks freely, joins conversations and answers when she has something to say. |
| `discord.mention_only_channels` | Channels where she speaks only when someone really `@mentions` her. Her name, replies to her and chatter do not wake her. |
| `discord.only_guilds` | Restrict her to these servers. Empty means every server she is in. |

In any other channel she answers when mentioned, replied to or called by name, and never joins in on her own. For a new, nervous server, start with a single mention-only channel.

## 5. Run her and add her to a server

```bash
mak1zu run
```

Open `http://127.0.0.1:8787`. The first-run checklist on the **Live** tab shows what is still missing, and once the token is in it builds the **invite link** for you. Open it, pick a server you manage, authorize. She appears online.

Everything in the panel applies live and writes `config.json` for you: models, rooms, behavior dials, her personality file, house rules. Secrets are write-only there.

## 6. Keep her running

```bash
mak1zu service > ~/.config/systemd/user/mak1zu.service
systemctl --user enable --now mak1zu
journalctl --user -u mak1zu -f
```

The unit is sandboxed (`ProtectSystem=strict`, no new privileges) and can only write to `.makizu/`. Only one Mak1zu may run on a data directory; a second refuses to start, because two Discord clients means double replies.

Docker: the `Dockerfile` builds a distroless image. Inside a container the panel has to listen on `0.0.0.0`, which requires `web_ui.token`; publish the port on `127.0.0.1` only.

## When something is off

| Symptom | Likely cause |
| --- | --- |
| She is online but never replies | Message Content Intent is off, or the channel is mention-only and nobody mentioned her. The **Live** tab shows every message she ignored and why. |
| `mak1zu doctor` fails on the provider | Read the fix line. [PROVIDERS.md](PROVIDERS.md#when-it-breaks) has the common ones. |
| Slash commands do not show up | Discord can take a minute to publish global commands. `discord.register_commands` must be on. |
| She answers twice | Two instances share one token. Stop the other one. |
| She sounds like customer support | Fix the persona file, not the code: [PERSONA_TEMPLATE.md](PERSONA_TEMPLATE.md). |
