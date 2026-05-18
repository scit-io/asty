.PHONY: build build-demo build-all clean test test-integration vet race tidy ci run-agent run-server fmt lint deps dist nats-server

# Pinned to match the github.com/nats-io/nats-server/v2 line in go.mod.
# Bump both together so the JSON shapes the agent decodes from
# $SYS.SERVER.<id>.STATSZ keep matching the running server.
NATS_SERVER_VERSION = 2.14.0

# Build orchestrator binary
build:
	go build -o bin/asty ./asty/cmd

# Build demo service binaries
build-demo:
	go build -o bin/xauth ./demo/cmd/xauth
	go build -o bin/xhttp ./demo/cmd/xhttp
	go build -o bin/xws ./demo/cmd/xws

# Fetch the nats-server binary the agent supervises at startup. No-op
# when bin/nats-server already matches the pinned version.
nats-server:
	@if [ ! -x bin/nats-server ] || ! bin/nats-server --version 2>/dev/null | grep -q "$(NATS_SERVER_VERSION)"; then \
		echo "downloading nats-server v$(NATS_SERVER_VERSION)..."; \
		mkdir -p bin; \
		OS=$$(uname -s | tr A-Z a-z); \
		ARCH=$$(uname -m); \
		case "$$ARCH" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; esac; \
		TAR=nats-server-v$(NATS_SERVER_VERSION)-$${OS}-$${ARCH}.tar.gz; \
		curl -fsSL -o /tmp/$$TAR "https://github.com/nats-io/nats-server/releases/download/v$(NATS_SERVER_VERSION)/$$TAR"; \
		tar -xzf /tmp/$$TAR -C /tmp; \
		cp /tmp/nats-server-v$(NATS_SERVER_VERSION)-$${OS}-$${ARCH}/nats-server bin/nats-server; \
		chmod +x bin/nats-server; \
		rm -rf /tmp/$$TAR /tmp/nats-server-v$(NATS_SERVER_VERSION)-$${OS}-$${ARCH}; \
	fi

# Build everything (asty + demo + the nats-server binary).
build-all: build build-demo nats-server

# Clean build artifacts
clean:
	rm -rf bin/ dist/

# Run unit tests (fast, default for everyday work)
test:
	go test ./...

# Run integration tests — gateway suite under //go:build integration
# spins up an embedded NATS server and exercises the full HTTP→NATS
# round trip. Slower than the unit suite, kept behind the tag so the
# default `make test` stays snappy.
test-integration:
	go test -tags=integration -count=1 ./...

# Run `go vet ./...` — one of the four mandatory checks listed in
# .claude/coding-rules/testing.md. Must be clean before commit.
vet:
	go vet ./...

# Run tests under the race detector. Concurrency is the rule in this
# repo (streamHub, controller workqueue, agent restart loop, drain
# manager, process monitor); race failures are blocking.
race:
	go test -race -count=1 ./...

# Tidy go.mod / go.sum. Run after adding or removing dependencies.
tidy:
	go mod tidy

# layer-check enforces TZ §2.9: env reads outside core/config are
# forbidden. Only os.Getenv/os.LookupEnv match — os.Setenv is fine,
# it's how the agent pushes env to its child processes. The grep
# pattern requires `(` to skip references inside comments. depguard
# in .golangci.yml documents the same rule for editors.
layer-check:
	@bad=$$(grep -rnE '(os\.Getenv|os\.LookupEnv)\(' asty/internal/ --include='*.go' | grep -v '_test\.go' | grep -v 'asty/internal/core/config/' || true); \
	if [ -n "$$bad" ]; then echo "layer-check: env reads outside core/config detected"; echo "$$bad"; exit 1; fi

# CI aggregator — runs the four mandatory checks from
# testing.md plus integration tests plus the layer-check.
ci: build vet race test-integration layer-check

# Run agent
run-agent: build
	./bin/asty -mode agent

# Run server
run-server: build
	./bin/asty -mode server

# Format code
fmt:
	go fmt ./...

# Lint code — third-party golangci-lint, kept separate from `ci`
# so the aggregator does not require an extra tool on the machine.
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod download
	go mod tidy

# Cross-compile orchestrator
dist:
	GOOS=linux GOARCH=amd64 go build -o dist/asty_linux_amd64 ./asty/cmd
	GOOS=linux GOARCH=arm64 go build -o dist/asty_linux_arm64 ./asty/cmd
	GOOS=darwin GOARCH=amd64 go build -o dist/asty_darwin_amd64 ./asty/cmd
	GOOS=darwin GOARCH=arm64 go build -o dist/asty_darwin_arm64 ./asty/cmd
