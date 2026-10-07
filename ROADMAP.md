# Roadmap

Where things stand, honestly. "Verified" means run against real Discord or a
real provider, not just unit tests.

## Works today

Engine, memory, guard, policy, providers with presets and diagnostics, web
panel, Discord and terminal transports, slash commands, MCP tools, the
`.makizu/` rules system, the SDK. Offline tests pass with `-race`; the binary is
static and CGO-free.

Verified live: replying to a mention, silence on ambient chatter (with the
reason shown), English by default and switching to the speaker's language,
reminders firing, files and GIFs, a provider failing and being fixed from the
panel, the pause switch, six slash commands registering.

## Not verified yet

DMs and the owner-only `/persona` gate, vision, peer bots, mention-only rooms on
a live server, splitting a long reply, catch-up after a restart, browser clicks
on every panel control, packaging.

## Next, in order

1. **Close the "not verified" list** on a staging server.
2. **Provider presets stay true.** Models are retired every few months. A
   scheduled check that every preset's model still appears in its provider's
   model list.
3. **Companion mode, delivery half (opt-in):** the night shift already decides what she
   would like to say and she raises it in the next private chat; what is missing is her
   starting the conversation, on the transport you were last on, honouring the
   per-person `checkins` and `quiet` boundaries that already exist.
4. **Weeb toolbox, rest:** wallpapers, "what should I watch" from someone's AniList.
5. **Embeddings:** an optional local embedder behind `memory.Embedder`.
6. **Eval corpus:** grow `examples/eval-inputs.txt` and add a repetition and
   opener-variety metric to `mak1zu eval`.
7. **Packaging:** `.goreleaser.yaml`, the `Dockerfile` and `packaging/arch/PKGBUILD`
   exist and stay unbuilt until the first tagged release; a Nix flake is missing.
8. **Other transports:** Matrix, Telegram, IRC.
