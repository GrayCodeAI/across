.PHONY: build test test-e2e test-race vet e2e fuzz fmt fmt-check coverage cross-check vulncheck clean check install-local uninstall-local

GOVULNCHECK_VERSION ?= v1.8.0

fmt:
	gofmt -w .

fmt-check:
	@unformatted="$$(gofmt -l .)"; if [ -n "$$unformatted" ]; then echo "gofmt required for:"; echo "$$unformatted"; exit 1; fi

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

test-e2e:
	go test -count=1 -run E2E ./...

test-race:
	go test -race -count=1 ./...

vet:
	go vet ./...

e2e: test-e2e

fuzz:
	go test -fuzz=FuzzAcrossJSONL -fuzztime=15s ./internal/event/

coverage:
	go test -count=1 -coverprofile=coverage.out ./...

cross-check:
	GOOS=windows GOARCH=amd64 go vet ./...
	GOOS=windows GOARCH=amd64 go build ./...

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

clean:
	rm -rf bin/

check: fmt-check vet test-race

install-local:
	mkdir -p $(HOME)/.local/bin
	cp bin/across $(HOME)/.local/bin/
	cp bin/across-agent-* $(HOME)/.local/bin/ || true

uninstall-local:
	rm -f $(HOME)/.local/bin/across $(HOME)/.local/bin/across-agent-*
