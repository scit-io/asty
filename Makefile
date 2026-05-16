.PHONY: build build-demo build-all clean test test-integration vet race tidy ci run-agent run-server fmt lint deps dist

# Build orchestrator binary
build:
	go build -o bin/asty ./asty/cmd

# Build demo service binaries
build-demo:
	go build -o bin/xauth ./demo/cmd/xauth
	go build -o bin/xhttp ./demo/cmd/xhttp
	go build -o bin/xws ./demo/cmd/xws

# Build everything
build-all: build build-demo

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

# CI aggregator — runs the four mandatory checks from
# testing.md plus integration tests. A green `make ci` is what
# every commit should produce.
ci: build vet race test-integration

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
