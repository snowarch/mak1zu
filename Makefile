.PHONY: build test vet run install
build:
	go build -trimpath -ldflags "-s -w" -o bin/mak1zu ./cmd/mak1zu
test:
	go test -race ./...
vet:
	go vet ./...
install: build
	install -Dm755 bin/mak1zu $(HOME)/.local/bin/mak1zu
run: build
	./bin/mak1zu run
