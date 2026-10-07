# Roadmap

Where things stand, honestly. "Verified" means run against real Discord or a
real provider, not just unit tests.

## Works today

The engine (policy, turn pipeline, guard, memory, reminders, tools, MCP, the
`.makizu/` rules system, the SDK), providers with presets and diagnostics, and
these companion parts:

- **One person across places.** Memory belongs to a person, not an account:
  `/link` joins a Discord account and the terminal into one person. Owner is a
  role. She asks what to call you once, in private.
- **A ledger, not a blob.** What she learned and where, what is still in flight
  in your life, running jokes that rest between callbacks, how you want to be
  treated. Readable and deletable: `/memories`, `/diary`, the panel's People tab.
- **A night shift (opt-in).** Tidies memory, writes a private diary, picks what
  she would like to bring up. `mak1zu night --dry-run` shows it without storing.
- **She can write first (opt-in)**, in a private chat you started, with quiet
  hours, a daily cap, back-off when ignored and an honoured stop.
- **Several transports in one engine.** Discord, the terminal chat (`mak1zu`),
  and the panel's Chat tab are the same conversation.
- **The workshop.** On request she drafts a character, skill, rule or dial
  change, shows the diff, and applies it after your yes in a later message.
- **A measurable voice.** `mak1zu eval --gate`, `persona distill`, soul packs.
- **Budgets.** `engine/budget_test.go` fails when every turn gets heavier.

Verified live: terminal chat attached to a running daemon with a real model, a
workshop change drafted and applied through it, the night shift on real
conversations, the chosen name stored and reused, a persona distilled from a
synthetic export. Mention replies, silence with reasons, reminders, files,
vision, web search and wallpapers on Discord (earlier sessions).

## Not verified yet

`/link`, `/callme` and `/diary` as real Discord slash commands; her writing
first on Discord (needs one DM so a private route exists); unsaid items
surfacing in a real DM; clicking every panel control in a browser (screenshots
and API tests only); DMs and the owner-only `/persona` gate; peer bots;
mention-only rooms on a live server; long-reply splitting; catch-up after a
restart; Windows and macOS; packaging.

## Next, in order

1. **Close the "not verified" list** on a staging server, with the owner.
2. **Per-person mood.** The night shift writes a diary and things to say, but the
   mood is still one global, keyword-driven value.
3. **`why` on every reply:** which memories, rule and token split went in, in
   the live feed.
4. **Provider presets stay true.** A scheduled check that every preset's model
   still appears in its provider's list.
5. **Embeddings:** an optional local embedder behind `memory.Embedder`.
6. **Weeb toolbox, rest:** "what should I watch" from someone's AniList.
7. **Packaging:** `.goreleaser.yaml`, the `Dockerfile` and `packaging/arch/PKGBUILD`
   exist and stay unbuilt until the first tagged release; a Nix flake is missing.
8. **Other transports:** Matrix, Telegram, IRC, as thin `sdk.Transport`s.
