.PHONY: build clean test run-agent run-server fmt lint

# Build binary
build:
	go build -o asty ./cmd/asty

# Clean build artifacts
clean:
	rm -f asty

# Run tests
test:
	go test -v ./...

# Run agent
run-agent: build
	./asty -mode agent

# Run server
run-server: build
	./asty -mode server

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

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o dist/asty_linux_amd64 ./cmd/asty
	GOOS=linux GOARCH=arm64 go build -o dist/asty_linux_arm64 ./cmd/asty
	GOOS=darwin GOARCH=amd64 go build -o dist/asty_darwin_amd64 ./cmd/asty
	GOOS=darwin GOARCH=arm64 go build -o dist/asty_darwin_arm64 ./cmd/asty
