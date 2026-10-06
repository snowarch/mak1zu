# Contributing

1. `make test` must pass (`go test -race ./...`, offline). `CGO_ENABLED=0 go build ./cmd/mak1zu` must keep working.
2. Read [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) invariants and [docs/SECURITY.md](docs/SECURITY.md) rules first.
3. Fix bugs with a test that uses the real malformed shape *and* the nearest legitimate case, so a guard never gets broader than the bug.
4. Do not solve a persona problem in code or a code problem in a persona. Tics are `guard`'s job; voice is the character file's.
5. Never add a tool that reads files or runs commands from chat.
6. Don't commit `.env`, `config.json`, `data/`, or voice exports.
7. Commit messages: short, lowercase, one line, what changed (`fix null required in tool schemas`).
