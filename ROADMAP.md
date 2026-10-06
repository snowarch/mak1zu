# Roadmap

**v0.1 (this tree)**: engine, providers, memory, guard, policy, panel, Discord + CLI transports, persona packs, SDK. Offline tests green with `-race`; static CGO-free binary.

## Next, in order

1. **Live Discord proof.** Run a staging bot in a test server and verify the
   full path (mention, reply, DM, mention-only channel, emoji, split, files,
   catch-up). Unit tests do not prove live behavior.
2. ~~Slash commands, MCP client, safe `write_file`, AniList tools, `mak1zu eval` voice harness~~: done in v0.1 and verified offline; Discord-side command registration still needs step 1.
3. **Weeb toolbox, rest**: GIF/reaction search, wallpapers, "what should I watch" from a user's AniList.
4. **Companion mode (opt-in)**: proactive check-ins with hard rate limits, a
   quiet-hours window, and an easy "stop" the persona honors.
5. **Embeddings**: optional local embedder (ONNX) behind `memory.Embedder`.
6. **Eval corpus**: `mak1zu eval` exists; grow `examples/eval-inputs.txt` from the exported voice corpus.
7. **Packaging**: `.goreleaser.yaml`, `Dockerfile`, `packaging-PKGBUILD` are written but unbuilt until a tagged release exists; Nix flake still missing.
8. **Other transports**: Matrix, Telegram, IRC, a local desktop widget.
