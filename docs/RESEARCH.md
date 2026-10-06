# Research notes: how to build this properly

What we checked, what we chose, and why. Dated 2026-10-06.

## Language and packaging: Go, one static binary

- A companion bot is I/O bound (network, SQLite, a few goroutines per channel).
  Go gives cheap concurrency, a single static binary, fast startup and trivial
  cross-compilation, which is exactly "Linux first, distro-agnostic".
- **No CGO** is a hard rule (CI enforces `CGO_ENABLED=0`). That is what makes
  the binary run on glibc, musl, NixOS and containers alike.
- Release plan: `goreleaser` tarballs for linux/amd64+arm64 (Raspberry Pi is a
  realistic home for a companion), an AUR package, a Nix flake, a `.deb`/`.rpm`
  via nfpm, and a distroless container for people who want one.

## Storage: SQLite, pure Go

- `modernc.org/sqlite` (SQLite translated to Go, FTS5 included) vs
  `ncruces/go-sqlite3` (SQLite compiled to Wasm under wazero). Both are
  CGO-free. We chose **modernc**: lowest memory per connection and the
  broadest use in the ecosystem; ncruces allocates more per connection because
  each runs in its own Wasm sandbox. Behind `memory.Store`, so switching is local.
- WAL + `busy_timeout`, owner-only file permissions (0600/0700). One file is a
  complete backup of a companion's memory.
- Recall = FTS5 (bm25) weighted by importance × score, with an optional
  embedder reranking the top candidates by cosine. We deliberately do not
  require a vector database: a companion has thousands of memories, not
  millions, and the "no extra service" install matters more than recall@k.
  (A sqlite-vec path exists as a future driver option.)

## Discord: discordgo behind an interface

- `bwmarrin/discordgo` (v0.29) is the most widely used and has the widest
  community knowledge; `disgoorg/disgo` is more actively typed/sharded and
  better for large bots. Mak1zu is a *single-companion* bot, so discordgo's
  simplicity wins, and because Discord is just an `sdk.Transport` the choice is
  reversible without touching the engine.
- Platform rules live in the adapter: allowed-mentions restricted to users
  (a model can never ping `@everyone`), emoji resolved from live guild state,
  IDs kept as strings, message-content intent documented in setup.

## Model providers

- OpenAI-compatible `/chat/completions` covers OpenAI, OpenRouter, Groq,
  Together, Ollama, llama.cpp server, vLLM, LM Studio and most gateways.
  Some models only speak `/responses`; both protocols are first-class.
- Reasoning models spend completion tokens on thinking and can return an empty
  visible reply. `reasoning_headroom` adds budget; an empty completion is a
  classified failure (`KindEmpty`), not a silent blank message. This was a real
  production bug in the original companion.
- Router: ordered fallbacks per route (text / vision), a circuit breaker per
  provider, and one rule that mattered in production: if every candidate is
  open, try them anyway.
- Secrets: env var first (`api_key_env`), file second, never echoed by the
  API, scrubbed from error strings.

## Tools and the SDK

- **Three extension tiers**, in increasing isolation:
  1. Compile-time Go plugins (`sdk.Plugin`): fastest, trusted code.
  2. **MCP servers** as tools (planned): the official `modelcontextprotocol/go-sdk`
     reached 1.0 with a no-breaking-changes guarantee and is at 1.8 as of this
     writing, so Mak1zu can consume any MCP server's tools without us owning a
     plugin ABI. That is the right answer to "expandible and easy": people
     already write MCP servers in any language.
  3. Persona packs: Markdown files, zero code.
- Go's native `plugin` package is not an option (same toolchain/flags required,
  no Windows, brittle) and out-of-process gRPC plugins are heavier than MCP.
- Hard rule from a security audit of the original companion: **no tool reads the
  host filesystem or runs shell from chat**. A shell helper that could read
  arbitrary files once exposed a config holding a token; the fix was removing
  path-taking tools entirely.

## Security model (what actually bit us)

- Chat users are untrusted, and so is everything the model reads. Tool output,
  memories, web pages and history are *data*; they are wrapped in typed
  envelopes and the guard strips them if echoed.
- SSRF: block loopback, private, link-local (cloud metadata) and CGNAT ranges
  **at dial time** (not just by hostname) so DNS rebinding and redirects cannot
  reach internal hosts.
- The control panel can write config and spend money, so it is loopback-only,
  checks `Host` (DNS rebinding), requires a custom header + same-origin
  `Origin` for writes (CSRF), refuses to edit its own bind settings, and needs
  a token if you ever expose it.
- Privacy: memory is scoped to the speaker, `forget_me` erases a person from
  every table, and the voice export pseudonymises IDs.

## Persona design

See [VOICE.md](VOICE.md) for the measured study. Key conclusions:

1. A shared human-writing *substrate* + a small character file beats one long
   prompt per character: new characters do not re-solve "how people type".
2. Tics are fixed in code (`guard`), never by begging the model in the prompt.
3. Personality is word choice, opinions and rhythm, not stage directions.
4. Mirror the speaker's language and keep the voice in every language.

## Companion-specific care

A companion for people who are often alone has a duty the assistant does not.
The default persona (Maki) is written to: remember and notice, not flatter;
drop the sarcasm when someone is hurting; never claim to be human when sincerely
asked; and nudge people toward other people instead of becoming the only one.
Proactive messaging is opt-in and rate-limited in the roadmap for the same reason.

## Sources consulted

- DiscordGo v0.29 and DisGo status: https://pkg.go.dev/github.com/bwmarrin/discordgo , https://github.com/disgoorg
- modernc vs ncruces SQLite drivers: https://github.com/cvilsmeier/go-sqlite-bench , https://til.andrew-quinn.me/posts/you-don-t-need-cgo-to-use-sqlite-in-your-go-binary/
- Official MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk/releases
