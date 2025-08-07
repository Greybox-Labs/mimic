# Justfile for mimic - API Record and Replay Tool

# Default recipe - show help
default:
    @just --list

# Build the binary
build:
    go build -o build/mimic .

# Build for multiple platforms
build-all:
    GOOS=linux GOARCH=amd64 go build -o build/mimic-linux-amd64 .
    GOOS=darwin GOARCH=amd64 go build -o build/mimic-darwin-amd64 .
    GOOS=darwin GOARCH=arm64 go build -o build/mimic-darwin-arm64 .
    GOOS=windows GOARCH=amd64 go build -o build/mimic-windows-amd64.exe .

# Install the binary to $GOPATH/bin
install:
    go install .

# Run the application with default config
run *args:
    go run . {{args}}

# Run tests
test:
    go test ./...

# Run integration tests
integration-test:
    ./integration_test.sh

# Run tests with coverage
test-coverage:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# Run tests with race detection
test-race:
    go test -race ./...

# Run benchmarks
bench:
    go test -bench=. ./...

# Format code
fmt:
    go fmt ./...

# Vet code
vet:
    go vet ./...

# Run golangci-lint
lint:
    golangci-lint run

# Install golangci-lint
install-lint:
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.55.2

# Tidy dependencies
tidy:
    go mod tidy

# Download dependencies
deps:
    go mod download

# Check for vulnerabilities
vuln:
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Clean build artifacts
clean:
    rm -f mimic
    rm -rf build/
    rm -f coverage.out coverage.html

# Generate documentation
docs:
    go run . --help > docs/usage.txt

# Create a new release build
release version:
    @echo "Creating release {{version}}"
    git tag {{version}}
    just build-all
    @echo "Release {{version}} created"

# Setup development environment
setup:
    go mod download
    just install-lint
    @echo "Development environment setup complete"

# Run quality checks (format, vet, lint, test)
check:
    just fmt
    just vet
    just lint
    just test

# Watch for changes and rebuild
watch:
    #!/bin/bash
    while true; do
        inotifywait -e modify,create,delete -r . --exclude '\.(git|build)' 2>/dev/null || fswatch -o . | head -1 >/dev/null
        echo "Rebuilding..."
        just build
    done

# Start all configured proxies
dev:
    go run . --config config.yaml

# Start web UI only
dev-web:
    go run . web --config config.yaml



# Export current session for testing
export-session session="default":
    go run . export --session {{session}} --output test-session.json

# Import session for testing
import-session file="test-session.json":
    go run . import --input {{file}} --session imported-test

# Clear all sessions
clear-sessions:
    go run . clear --all

# Show current sessions
list-sessions:
    go run . list-sessions

# Initialize project structure (for new installations)
init:
    mkdir -p ~/.mimic
    cp config.yaml ~/.mimic/config.yaml || true
    @echo "Mimic initialized. Config file created at ~/.mimic/config.yaml"

# Show project info
info:
    @echo "Project: mimic"
    @echo "Go version: $(go version)"
    @echo "Module: $(head -1 go.mod)"
    @echo "Build: $(ls -la mimic 2>/dev/null || echo 'Not built')"
