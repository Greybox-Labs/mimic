#!/bin/bash

# Mimic Installation Script
# This script builds and installs the mimic binary and sets up the ~/.mimic directory

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Print functions
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed. Please install Go 1.21 or later."
    exit 1
fi

# Check Go version
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
REQUIRED_VERSION="1.21"
if [[ "$(printf '%s\n' "$REQUIRED_VERSION" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_VERSION" ]]; then
    print_error "Go version $REQUIRED_VERSION or later is required. Current version: $GO_VERSION"
    exit 1
fi

print_info "Go version $GO_VERSION detected"

# Build the binary
print_info "Building mimic binary..."
go build -o mimic .

if [ $? -ne 0 ]; then
    print_error "Failed to build mimic binary"
    exit 1
fi

print_info "Build successful"

# Determine installation directory
if [[ "$EUID" -eq 0 ]]; then
    # Running as root, install system-wide
    INSTALL_DIR="/usr/local/bin"
    print_info "Installing system-wide to $INSTALL_DIR"
else
    # Running as regular user, install to user's bin directory
    INSTALL_DIR="$HOME/.local/bin"
    print_info "Installing to user directory $INSTALL_DIR"
    
    # Create the directory if it doesn't exist
    mkdir -p "$INSTALL_DIR"
    
    # Check if ~/.local/bin is in PATH
    if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
        print_warn "~/.local/bin is not in your PATH"
        print_warn "Add the following line to your ~/.bashrc or ~/.zshrc:"
        print_warn "export PATH=\"\$HOME/.local/bin:\$PATH\""
    fi
fi

# Copy binary to installation directory
print_info "Copying mimic to $INSTALL_DIR"
cp mimic "$INSTALL_DIR/mimic"

if [ $? -ne 0 ]; then
    print_error "Failed to copy mimic to $INSTALL_DIR"
    print_error "You may need to run this script with sudo for system-wide installation"
    exit 1
fi

# Make it executable
chmod +x "$INSTALL_DIR/mimic"

# Create ~/.mimic directory and default config
MIMIC_DIR="$HOME/.mimic"
print_info "Creating mimic directory at $MIMIC_DIR"
mkdir -p "$MIMIC_DIR"

# Create default config if it doesn't exist
CONFIG_FILE="$MIMIC_DIR/config.yaml"
if [ ! -f "$CONFIG_FILE" ]; then
    print_info "Creating default configuration at $CONFIG_FILE"
    cat > "$CONFIG_FILE" << 'EOF'
proxy:
  mode: "record" # record | mock
  target_host: "api.example.com"
  target_port: 443
  listen_host: "0.0.0.0"
  listen_port: 8080
  protocol: "https"

database:
  path: "~/.mimic/recordings.db"
  connection_pool_size: 10

recording:
  session_name: "default"
  capture_headers: true
  capture_body: true
  redact_patterns:
    - "Authorization: Bearer .*"
    - "X-API-Key: .*"

mock:
  matching_strategy: "exact" # exact | pattern | fuzzy
  sequence_mode: "ordered" # ordered | random
  not_found_response:
    status: 199
    body:
      error: "Recording not found"

grpc:
  proto_paths:
    - "./protos"
  reflection_enabled: true

export:
  format: "json"
  pretty_print: true
  compress: false
EOF
else
    print_info "Configuration file already exists at $CONFIG_FILE"
fi

# Test installation
print_info "Testing installation..."
if command -v mimic &> /dev/null; then
    print_info "Installation successful!"
    print_info "mimic version: $(mimic --help | head -n1)"
    print_info ""
    print_info "Configuration directory: $MIMIC_DIR"
    print_info "Default config file: $CONFIG_FILE"
    print_info ""
    print_info "To get started:"
    print_info "  mimic --help"
    print_info "  mimic --mode record --config ~/.mimic/config.yaml"
else
    print_error "Installation failed - mimic command not found in PATH"
    exit 1
fi

# Clean up build artifact in current directory
if [ -f "./mimic" ]; then
    rm "./mimic"
    print_info "Cleaned up build artifacts"
fi

print_info "Installation complete!"
