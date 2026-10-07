<p align="center"><img src="brand/png/banner.png" alt="Mak1zu" width="100%"></p>

<p align="center"><b>The companion you shape.</b><br>
A Go engine for a Discord companion that remembers people, has a real personality, and runs from one static binary on any Linux distro.</p>

---

Most chatbots sound like customer support wearing a costume. Mak1zu is built the other way round: a shared layer of rules for how people actually type in chat (short, opinionated, imperfect), a small Markdown file for who the character is, and a guard that strips the tells. The numbers behind those rules come from measuring thousands of real replies; they are in [docs/VOICE.md](docs/VOICE.md).

The default character is **Maki**: flat and dry on the surface, shy underneath. Swap her for anyone by editing one file.

## What you get

- **Servers and DMs, with house rules per room.** Home channels where she talks freely, mention-only channels where only a real `@mention` wakes her, owner-only DMs.
- **Memory that stays with the person.** SQLite with full-text search: what you told her, how she relates to you, inside jokes, reminders, `forget me`. One person's memory never reaches another person's prompt; a test proves it.
- **Any model.** Thirteen presets, or paste the address of anything that speaks the OpenAI API (vLLM, llama.cpp, LiteLLM, Together, a company gateway). Ordered fallbacks, a circuit breaker, vision routing, and `init` makes a real call to prove it works before it finishes: [docs/PROVIDERS.md](docs/PROVIDERS.md).
- **A web panel** on `127.0.0.1:8787`: live feed of what she heard and why she stayed quiet, models, rooms, dials, her personality file. Changes apply instantly; keys are write-only.
- **A guard on every reply.** No leaked prompts, no tool protocol, no `*stares*` ten times in a row, no "I'd be happy to help". A reply that promises an attachment and has none is caught.
- **Extensible.** Tools and hooks in about 20 lines of Go, any [MCP](https://modelcontextprotocol.io) server as a tool source, transports in five methods, personalities in Markdown. [docs/SDK.md](docs/SDK.md)
- **Slash commands:** `/persona` (owner), `/callme`, `/link`, `/mood`, `/remember`, `/memories`, `/forget`, `/ping`.

## What she can do

Every tool below is offered on every message; she decides when to use one. None needs a key or a server.

| Someone asks for | Tool | Where it comes from |
| --- | --- | --- |
| news, a fact, a release date, anything current | `web_search` | Exa, DuckDuckGo and Bing in turn; your own [SearXNG](https://docs.searxng.org) first if you set `search.searxng_url` |
| a summary of a link, a GitHub repo, a page | `read_url` | the page itself (public addresses only) |
| a wallpaper | `wallpaper` | wallhaven.cc, safe-for-work only (fixed in code) |
| a photo of something | `image_search` | Wikimedia Commons |
| a reaction GIF | `reaction_gif` | nekos.best |
| anime or manga info, what airs today | `anime_search`, `anime_airing` | AniList |
| a file (html, json, a script) | `write_file` | made on the spot and attached to her reply |
| remember me, remind me in 20 minutes | `remember`, `recall`, `set_reminder`, `forget_me` | her own memory |
| what is in a picture you posted | vision | needs a model that accepts images |
| anything else | any MCP server | `tools.mcp_servers` |

Links for images, wallpapers and GIFs always come from the service's own answer and are checked against its host: she cannot make one up or be steered to another site.

Checked live in a Discord channel on 2026-10-07 with a real model: web search (an answer that matched go.dev), reading a page, wallpaper, photo search, GIF, anime lookup, an attached `.html` file, memory, a reminder that fired on time, and an image she described correctly. Only unit-tested so far: `anime_airing` and MCP servers.

What she does not do: join voice channels, run commands or read files on the host (by design, see [docs/SECURITY.md](docs/SECURITY.md)), moderate people, or generate images.

## Quick start

```bash
git clone https://github.com/snowarch/mak1zu && cd mak1zu
make install         # builds and copies mak1zu to ~/.local/bin
mkdir ~/mak1zu && cd ~/mak1zu
mak1zu init          # pick a provider or paste your own URL, paste the key (hidden)
mak1zu doctor        # one real call; if it fails, it says what to fix
mak1zu chat          # try her in the terminal
mak1zu run           # Discord + panel at http://127.0.0.1:8787
```

Needs Go 1.27+. Putting her in a Discord server takes a bot token and one intent switch: [docs/SETUP.md](docs/SETUP.md) walks through it.

### Which model?

No card, no money:

| | Where | Catch |
| --- | --- | --- |
| `openrouter-free` | [OpenRouter](https://openrouter.ai/keys) `:free` models | 20 requests a minute and a daily cap; free endpoints may train on your prompts |
| `cline` | [Cline](https://app.cline.bot) API keys | free models rotate and have a quota |
| `ollama`, `lmstudio` | your own machine | slower, and nothing ever leaves it |

Cheap and good: `opencode-go` (a subscription to open-weight models; works from any client), `deepseek`, `gemini`, `groq`. Also `openai`, `anthropic`, `mistral`, `openrouter`, `opencode-zen`.

**Your own server or gateway.** The last entry of the `init` menu is "Your own URL": paste any OpenAI-compatible address (with or without `/v1`, or the whole `/chat/completions` URL), and it lists the models it serves and lets you pick one. Servers already running on your machine (Ollama, LM Studio, llama.cpp, vLLM, Jan, KoboldCpp, LiteLLM on their usual ports) show up in the menu by themselves. From a script:

```bash
mak1zu init --base-url https://api.together.xyz/v1 --model meta-llama/Llama-3.3-70B-Instruct-Turbo --key-env TOGETHER_API_KEY
mak1zu init --base-url http://localhost:8000/v1      # one model served: it picks it
```

One thing worth knowing before you go looking: opencode Zen's *free* models only work inside the OpenCode app and answer `403` from anywhere else, even with a key. Go and paid Zen models work. The details, the commands to list each provider's models, and what each error means are in [docs/PROVIDERS.md](docs/PROVIDERS.md). `mak1zu providers` prints the same table in your terminal, and `mak1zu init --provider <id>` skips the menu.

## Make her yours: the `.makizu/` folder

`mak1zu init` creates `.makizu/`, found like a `.git` by walking up from where you run things. It is her: plain Markdown you edit by hand or in the panel, re-read on every message, no restart.

```
.makizu/
  personas/maki/persona.md     who she is (pick the active one in the panel)
  rules/10-care.md             house rules, always in her prompt
  servers/<server-id>.md       rules for one server
  channels/<channel-id>.md     rules for one channel
  skills/anime-recs/SKILL.md   know-how she reads on demand
  config.json  .env  data/     settings, secrets, memory (never commit these)
```

Keeping up with new releases is one command, `mak1zu init --update`, and it never overwrites your edits ([docs/SETUP.md](docs/SETUP.md#updating)). Add a character by copying [docs/PERSONA_TEMPLATE.md](docs/PERSONA_TEMPLATE.md), then `mak1zu eval examples/eval-inputs.txt` and read what she says. Add a skill by creating a folder with a `SKILL.md`. Rules and skills outrank her moods but never her hard lines. [.makizu/README.md](.makizu/README.md) has the details.

## Docs

[Setup](docs/SETUP.md) · [Providers](docs/PROVIDERS.md) · [Architecture](docs/ARCHITECTURE.md) · [SDK](docs/SDK.md) · [Voice study](docs/VOICE.md) · [Security](docs/SECURITY.md) · [Research](docs/RESEARCH.md) · [Roadmap](ROADMAP.md) · [Contributing](CONTRIBUTING.md) · [Brand](brand/README.md)

## Develop

```bash
make check       # vet + go test -race + CGO-free build (offline, a few seconds)
make build       # static, CGO-free binary in bin/
```

## License

Apache-2.0 with a [NOTICE](NOTICE): if you redistribute or modify Mak1zu, keep the notice and credit "Based on Mak1zu by snowarch". The name and artwork in `brand/` are not covered by the license: see [brand/README.md](brand/README.md).
