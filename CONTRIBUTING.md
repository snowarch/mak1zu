# Contributing

1. `make check` must pass (`go vet`, `go test -race`, and a CGO-free build; all offline). There is no hosted CI, so this is the gate: run it before you open a PR.
2. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) invariants and [docs/SECURITY.md](docs/SECURITY.md) rules first.
3. Fix bugs with a test that uses the real malformed shape *and* the nearest legitimate case, so a guard never gets broader than the bug.
4. Do not solve a persona problem in code or a code problem in a persona. Tics are `guard`'s job; voice is the character file's.
5. Never add a tool that reads files or runs commands from chat.
6. Don't commit `.env`, `config.json`, `data/`, or voice exports.
7. Commit messages: short, lowercase, one line, what changed (`fix null required in tool schemas`).
8. Provider presets (`provider/presets.go`) only carry model ids you have checked against that provider's own `/models` list or docs. Say how you checked in the commit or PR, and update [docs/PROVIDERS.md](docs/PROVIDERS.md).
