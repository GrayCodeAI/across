.PHONY: build test test-race vet e2e fuzz check install-local uninstall-local

build:
	go build -o bin/across ./cmd/across
	go build -o bin/across-agent-claude-code ./cmd/across-agent-claude-code
	go build -o bin/across-agent-codex ./cmd/across-agent-codex
	go build -o bin/across-agent-cursor ./cmd/across-agent-cursor
	go build -o bin/across-agent-gemini ./cmd/across-agent-gemini
	go build -o bin/across-agent-opencode ./cmd/across-agent-opencode
	go build -o bin/across-agent-qwen ./cmd/across-agent-qwen
	go build -o bin/across-agent-factory-droid ./cmd/across-agent-factory-droid
	go build -o bin/across-agent-amp ./cmd/across-agent-amp
	go build -o bin/across-agent-goose ./cmd/across-agent-goose

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

e2e:
	go test -count=1 -run E2E ./...

fuzz:
	go test -fuzz=FuzzAcrossJSONL -fuzztime=15s ./internal/event/ || true

check: vet test-race

install-local:
	mkdir -p $(HOME)/.local/bin
	cp bin/across $(HOME)/.local/bin/
	cp bin/across-agent-* $(HOME)/.local/bin/ || true

uninstall-local:
	rm -f $(HOME)/.local/bin/across $(HOME)/.local/bin/across-agent-*
