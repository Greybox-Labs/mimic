# Mimic - API Record and Replay Tool

A transparent proxy for intercepting, recording, and mocking API calls. Supports both REST and gRPC protocols with SQLite storage and JSON export/import capabilities.

## Features

- **Transparent Proxy Mode**: Intercepts and records API requests/responses
- **Mock Server Mode**: Replays recorded interactions
- **Replay Mode**: Tests recorded interactions against live servers with timing and validation
- **Protocol Support**: REST (HTTP/HTTPS) and gRPC
- **SQLite Storage**: Reliable local storage with ordering preservation
- **JSON Export/Import**: Version control integration and data portability
- **Configurable Redaction**: Sensitive data protection
- **Flexible Matching**: Exact, pattern, and fuzzy request matching
- **Web UI**: Real-time monitoring and session management interface

## Installation

### Quick Install (Recommended)

```bash
# Clone the repository
git clone https://github.com/your-org/mimic.git
cd mimic

# Run the install script
./install.sh
```

The install script will:
- Build the mimic binary
- Install it to `/usr/local/bin` (if run as root) or `~/.local/bin` (for user installs)
- Create the `~/.mimic` directory with default configuration
- Set up proper permissions

### Manual Installation

#### Build from Source

```bash
go build -o mimic .
```

#### Install to System

```bash
# Build and install system-wide (requires sudo)
go build -o mimic .
sudo cp mimic /usr/local/bin/

# Or install to user directory
go build -o mimic .
mkdir -p ~/.local/bin
cp mimic ~/.local/bin/
```

#### Set up Configuration Directory

```bash
# Create the mimic directory
mkdir -p ~/.mimic

# Copy default configuration
cp config.yaml ~/.mimic/config.yaml
```

### Run with Go (Development)

```bash
go run main.go [command] [flags]
```

### Verify Installation

```bash
mimic --help
```

## Configuration

Mimic uses a configuration file located at `~/.mimic/config.yaml` by default. The install script creates this file automatically, but you can also create it manually:

```yaml
server:
  listen_host: "0.0.0.0"
  listen_port: 8080
  grpc_port: 9080  # Optional: defaults to listen_port + 1000

proxies:
  api1:
    mode: "record"  # record | mock
    target_host: "api.example.com"
    target_port: 443
    protocol: "https"
    session_name: "api1-session"
  
  api2:
    mode: "mock"
    protocol: "http"
    session_name: "api2-mocks"
  
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
  matching_strategy: "exact"  # exact | pattern | fuzzy
  sequence_mode: "ordered"    # ordered | random
  not_found_response:
    status: 404
    body:
      error: "Recording not found"
      
export:
  format: "json"
  pretty_print: true
  compress: false
```

## gRPC Support

Mimic now provides full gRPC proxy functionality for recording and replaying gRPC interactions. This includes support for unary and streaming RPCs with automatic protobuf message handling.

### gRPC Configuration

Configure gRPC proxies by setting the protocol to "grpc":

```yaml
proxies:
  grpc-api:
    mode: "record"       # or "mock"
    protocol: "grpc"     # Set to grpc for gRPC support
    target_host: "api.grpc-service.com"
    target_port: 9090
    session_name: "grpc-session"

grpc:
  proto_paths:           # Paths to .proto files (optional)
    - "./protos"
    - "/usr/local/include"
  reflection_enabled: true  # Enable gRPC reflection (default: true)
```

### gRPC Recording

Record gRPC interactions by running mimic in record mode with a gRPC-configured proxy:

```bash
# Start gRPC recording
mimic --config config-grpc.yaml

# Your gRPC client should connect to localhost:9080 (configurable grpc_port) instead of the original server
# Mimic will forward the calls and record all interactions
# Example: if HTTP server runs on :8080, gRPC proxy will be on :9080 by default
```

### gRPC Mocking

Replay recorded gRPC interactions:

```bash
# Switch to mock mode in your config or use command line
mimic --mode mock --config config-grpc.yaml
```

### gRPC Features

- **Unary RPCs**: Full support for request/response recording and replay
- **Streaming RPCs**: Support for server streaming, client streaming, and bidirectional streaming
- **Metadata Handling**: Records and replays gRPC metadata (headers)
- **Protobuf Messages**: Automatically converts protobuf messages to JSON for storage
- **Service Reflection**: Supports gRPC server reflection for dynamic service discovery
- **Error Handling**: Records and replays gRPC status codes and error messages

### Example gRPC Workflow

1. **Record gRPC calls**:
   ```bash
   # Start mimic with gRPC proxy configuration
   mimic --config config-grpc-example.yaml
   
   # Your gRPC client should connect to the gRPC proxy port (configurable grpc_port)
   # If HTTP server is on :8080, gRPC proxy will be on :9080 by default
   buf curl --schema buf.build/your/api --protocol grpc localhost:9080/your.service/Method
   ```

2. **Export recorded session**:
   ```bash
   mimic export --session "grpc-session" --output "grpc-mocks.json"
   ```

3. **Use in tests**:
   ```bash
   # Import the recorded session
   mimic import --input "grpc-mocks.json" --session "test-grpc"
   
   # Start mock server (gRPC mock will be available on port 9080)
   mimic --mode mock --config config-grpc-example.yaml
   ```

## Usage

### Record Mode

Start the proxy in recording mode to capture API interactions:

```bash
# Using default config file (~/.mimic/config.yaml)
mimic

# Using custom config file
mimic --config /path/to/config.yaml

# Override mode via command line
mimic --mode record
```

Configure your application to use the proxy:
- HTTP Proxy: `http://localhost:8080`
- Direct API calls: Point to `http://localhost:8080` instead of the original API

### Mock Mode

Start the proxy in mock mode to serve recorded responses:

```bash
mimic --mode mock
```

### Replay Mode

Replay recorded interactions against a live server for testing and validation:

```bash
# Replay a session against a target server
mimic replay --session "my-session" --target-host api.example.com --target-port 443

# Replay with different validation strategies
mimic replay --session "api-tests" --target-host staging.api.com --matching-strategy fuzzy

# Replay with concurrent requests (faster execution)
mimic replay --session "load-test" --target-host localhost --target-port 8080 --concurrency 5

# Replay ignoring original timing (fire all requests immediately)
mimic replay --session "quick-test" --target-host api.test.com --ignore-timestamps

# Replay with fail-fast mode (exit on first mismatch)
mimic replay --session "critical-test" --target-host prod.api.com --fail-fast

# Replay gRPC interactions
mimic replay --session "grpc-session" --target-host localhost --target-port 9090 --protocol grpc

# Replay gRPC interactions with larger message sizes (for production servers)
mimic replay --session "grpc-session" --target-host grpc.production.com --protocol grpc --grpc-max-message-size 268435456

# Replay gRPC interactions with insecure connection (for testing)
mimic replay --session "grpc-session" --target-host localhost --target-port 9090 --protocol grpc --grpc-insecure
```

#### Replay Configuration Options

- `--session`: Session name to replay (required)
- `--target-host`: Target server hostname (required)
- `--target-port`: Target server port (default: 443)
- `--protocol`: Protocol to use - http, https, or grpc (default: https)
- `--matching-strategy`: Response validation strategy:
  - `exact`: Responses must match exactly (default)
  - `fuzzy`: Allow minor differences in JSON structure
  - `status_code`: Only validate HTTP status codes
- `--fail-fast`: Exit on first validation failure (default: false)
- `--timeout`: Request timeout in seconds (default: 30)
- `--concurrency`: Max concurrent requests (default: 0 for sequential)
- `--ignore-timestamps`: Skip timing-based replay (default: false)
- `--insecure-skip-verify`: Skip TLS verification for HTTPS/gRPC (default: false)
- `--grpc-max-message-size`: Max gRPC message size in bytes (default: 256MB)
- `--grpc-insecure`: Use insecure gRPC connection without TLS (default: false)

#### Replay via Server Mode

You can also run replay through the HTTP server interface:

```bash
# Start server in replay mode
mimic --mode replay --config config-replay.yaml

# Trigger replay via HTTP
curl -X POST "http://localhost:8080/proxy/replay-api/?session=my-session&target_host=api.example.com"
```

#### gRPC Replay

gRPC replay works by:
1. Establishing a gRPC client connection to the target server
2. Replaying recorded gRPC calls using the stored raw protobuf message data
3. Comparing responses based on the selected matching strategy
4. Supporting both unary and streaming RPCs (though streaming is sequential only)

**gRPC Replay Considerations:**
- gRPC replay uses TLS by default for production servers; use `--grpc-insecure` for local testing
- Raw protobuf message data is replayed exactly as recorded
- Status codes are compared using gRPC status codes (0 = OK, etc.)
- Metadata (gRPC headers) from the original requests is preserved and replayed
- Default message size limit is 256MB; increase with `--grpc-max-message-size` for larger messages
- For exact matching, both status code and response message must match
- For fuzzy matching, only status codes are compared
- Concurrent replay is supported for unary calls but not recommended for order-sensitive services
- Use `--insecure-skip-verify` to skip TLS certificate verification for testing environments

### Export Session

Export recorded session data to JSON:

```bash
mimic export --session "my-session" --output "session-data.json"
```

### Web UI

Mimic includes a web-based interface for monitoring and managing sessions:

```bash
# Start web UI only
mimic web

# Start all configured proxies with web UI
mimic

# Start with a custom config file
mimic --config custom-config.yaml
```

The web UI provides:
- **Real-time monitoring**: View incoming requests and responses as they happen
- **Session management**: Browse, inspect, and manage recorded sessions
- **Interactive exploration**: Click on interactions to see full request/response details
- **Live filtering**: Filter events by session or other criteria

Access the web UI at `http://localhost:8080/` (same port as the server). Multiple named proxies are available at `/proxy/<proxy_name>/` paths.

For example, with the configuration above:
- Web UI: `http://localhost:8080/`
- API1 proxy: `http://localhost:8080/proxy/api1/`
- API2 proxy: `http://localhost:8080/proxy/api2/`

### Import Session

Import session data from JSON:

```bash
# Import to new session
mimic import --input "session-data.json"

# Import to specific session
mimic import --input "session-data.json" --session "test-session"

# Replace existing session
mimic import --input "session-data.json" --merge-strategy replace
```

### List Sessions

View all recorded sessions:

```bash
mimic list-sessions
```

### Clear Session

Remove all data for a specific session:

```bash
mimic clear --session "my-session"
```

## Examples

### Recording API Calls

1. Start proxy in record mode:
   ```bash
   mimic --mode record
   ```

2. Configure your application to use `http://localhost:8080` as the API endpoint

3. Make API calls through your application

4. Export the recorded session:
   ```bash
   mimic export --session "default" --output "api-mocks.json"
   ```

### Using Mock Data in Tests

1. Import your recorded session:
   ```bash
   ./proxy-intercept import --input "api-mocks.json" --session "test-session"
   ```

2. Start proxy in mock mode:
   ```bash
   ./mimic --mode mock --config config.yaml
   ```

3. Run your tests pointing to `http://localhost:8080`

### CI/CD Integration

```yaml
# GitHub Actions example
- name: Start Mock Server
  run: |
    mimic import --input ./tests/api-mocks.json --session "ci-tests"
    mimic --mode mock &
    
- name: Run Integration Tests
  run: |
    npm test
  env:
    API_BASE_URL: http://localhost:8080
```

## Configuration Options

### Proxy Settings

- `mode`: Operation mode (`record`, `mock`, or `replay`)
- `target_host`: Target server hostname (required in record mode)
- `target_port`: Target server port (required in record mode)
- `listen_host`: Proxy listen address (default: `0.0.0.0`)
- `listen_port`: Proxy listen port (default: `8080`)
- `protocol`: Target protocol (`http` or `https`)

### Recording Settings

- `session_name`: Name for the recording session
- `capture_headers`: Whether to capture request/response headers
- `capture_body`: Whether to capture request/response bodies
- `redact_patterns`: Regex patterns for sensitive data redaction

### Mock Settings

- `matching_strategy`: Request matching strategy (`exact`, `pattern`, `fuzzy`)
- `sequence_mode`: Response selection mode (`ordered`, `random`)
- `not_found_response`: Default response for unmatched requests

### Replay Settings

- `target_host`: Target server hostname for replay
- `target_port`: Target server port for replay
- `protocol`: Target server protocol (`http`, `https`, or `grpc`)
- `session_name`: Session to replay
- `matching_strategy`: Response validation strategy (`exact`, `fuzzy`, `status_code`)
- `fail_fast`: Exit on first mismatch (boolean)
- `timeout_seconds`: Request timeout in seconds
- `max_concurrency`: Maximum concurrent requests (0 for sequential)
- `ignore_timestamps`: Skip timing-based replay (boolean)
- `insecure_skip_verify`: Skip TLS verification (boolean)
- `grpc_max_message_size`: Max gRPC message size in bytes
- `grpc_max_header_size`: Max gRPC header size in bytes
- `grpc_insecure`: Use insecure gRPC connection (boolean)

### Export Settings

- `format`: Export format (currently only `json`)
- `pretty_print`: Format JSON output for readability
- `compress`: Compress export files with gzip

## Matching Strategies

### Exact Match
Matches requests with identical method and endpoint.

### Pattern Match
Supports regex patterns in endpoint matching for dynamic URLs.

### Fuzzy Match
Intelligent matching that treats numeric IDs and UUIDs as equivalent.

## Data Redaction

Configure patterns to redact sensitive information:

```yaml
redact_patterns:
  - "Authorization: Bearer .*"
  - "X-API-Key: .*"
  - "password.*"
```

## Project Structure

```
mimic/
├── cmd/           # CLI commands
├── config/        # Configuration management
├── storage/       # Database models and operations
├── proxy/         # Proxy engine and REST handler
├── mock/          # Mock engine
├── export/        # Export/import functionality
├── main.go        # Application entry point
├── config.yaml    # Sample configuration file
├── install.sh     # Installation script
└── README.md      # This file
```

### User Data Directory

After installation, mimic creates a `~/.mimic` directory containing:

```
~/.mimic/
├── config.yaml    # User configuration
├── recordings.db  # SQLite database with recorded sessions
└── .gitignore     # Git ignore file for database files
```

## Development

This project uses [just](https://github.com/casey/just) as a command runner for common development tasks.

### Setup Development Environment

```bash
# Install just (if not already installed)
curl --proto '=https' --tlsv1.2 -sSf https://just.systems/install.sh | bash -s -- --to ~/bin

# Setup development environment
just setup
```

### Common Development Commands

```bash
# Show all available commands
just

# Build the binary
just build

# Run quality checks (format, vet, lint, test)
just check

# Start all configured proxies
just dev

# Format code
just fmt

# Run tests
just test

# Run tests with coverage
just test-coverage

# Run linting
just lint

# Clean build artifacts
just clean

# Show project info
just info
```

### Development Workflow

1. Make your changes
2. Run `just check` to ensure code quality
3. Run `just test` to verify functionality
4. Run `just build` to create the binary

### Building for Multiple Platforms

```bash
# Build for all supported platforms
just build-all

# Create a release
just release v1.0.0
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License.