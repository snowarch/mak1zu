What changed and why, in a couple of lines.

- [ ] `go vet ./... && go test -race ./...` pass, and `CGO_ENABLED=0 go build ./cmd/mak1zu` works
- [ ] a bug fix comes with a test using the real malformed shape and the nearest legitimate case
- [ ] no secrets, no `config.json`, no `.env`, no personal IDs
