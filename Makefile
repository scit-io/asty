.PHONY: build build-demo clean test run-agent run-server fmt lint

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

# Run tests
test:
	go test ./...

# Run agent
run-agent: build
	./bin/asty -mode agent

# Run server
run-server: build
	./bin/asty -mode server

# Format code
fmt:
	go fmt ./...

# Lint code
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
