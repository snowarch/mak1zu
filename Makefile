.PHONY: build test vet check run install
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/mak1zu ./cmd/mak1zu
test:
	go test -race ./...
vet:
	go vet ./...
check: vet test
	CGO_ENABLED=0 go build -o /dev/null ./cmd/mak1zu
install: build
	install -Dm755 bin/mak1zu $(HOME)/.local/bin/mak1zu
run: build
	./bin/mak1zu run
