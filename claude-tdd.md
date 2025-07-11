# API Proxy Intercept & Mock Server - Technical Design Document

## 1. Executive Summary

This document outlines the design for a binary application that serves as a transparent proxy for intercepting, recording, and mocking API calls. The system supports both REST and gRPC protocols, stores interaction data in SQLite, and provides import/export capabilities for version control integration.

### Key Capabilities
- Transparent proxy mode for API request/response recording
- Mock server mode for replaying recorded interactions
- Support for both REST and gRPC protocols
- SQLite-based storage with ordering preservation
- JSON export/import for CI/CD integration

## 2. System Architecture

### 2.1 High-Level Architecture

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │────▶│  Proxy Binary    │────▶│ External Server │
│Application  │◀────│                  │◀────│     (Target)    │
└─────────────┘     │  ┌────────────┐  │     └─────────────────┘
                    │  │   SQLite   │  │
                    │  │  Database  │  │
                    │  └────────────┘  │
                    │  ┌────────────┐  │
                    │  │JSON Export │  │
                    │  │  /Import   │  │
                    │  └────────────┘  │
                    └──────────────────┘
```

### 2.2 Operation Modes

1. **Proxy Mode**: Intercepts requests, forwards to target server, records interactions
2. **Mock Mode**: Intercepts requests, returns recorded responses from database

### 2.3 Core Components

- **Protocol Handler**: Manages REST/gRPC protocol-specific logic
- **Proxy Engine**: Handles request forwarding and response capture
- **Storage Manager**: Manages SQLite database operations
- **Mock Engine**: Matches requests and serves recorded responses
- **Export/Import Manager**: Handles JSON serialization/deserialization
- **Configuration Manager**: Manages runtime configuration

## 3. Component Design

### 3.1 Protocol Handler

Responsible for protocol-specific request/response handling.

**REST Handler**
- HTTP/HTTPS request parsing
- Header preservation
- Body content handling (JSON, XML, form-data, etc.)
- Response status and header management

**gRPC Handler**
- Protocol buffer message parsing
- Metadata handling
- Streaming support (unary, server streaming, client streaming, bidirectional)
- Service method identification

### 3.2 Proxy Engine

**Request Flow**
1. Receive incoming request
2. Extract protocol-specific details
3. Generate unique request identifier
4. Forward to target server
5. Capture response
6. Store interaction in database
7. Return response to client

**Key Features**
- Connection pooling for performance
- Timeout handling
- SSL/TLS support with certificate management
- Request/response transformation hooks

### 3.3 Storage Manager

**Database Schema Design**

```sql
-- Sessions table
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_name TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    description TEXT
);

-- Interactions table
CREATE TABLE interactions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    request_id TEXT UNIQUE NOT NULL,
    protocol TEXT NOT NULL CHECK(protocol IN ('REST', 'gRPC')),
    method TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    request_headers TEXT,
    request_body BLOB,
    response_status INTEGER,
    response_headers TEXT,
    response_body BLOB,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    sequence_number INTEGER NOT NULL,
    metadata TEXT,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- Indexes for performance
CREATE INDEX idx_endpoint_method ON interactions(endpoint, method);
CREATE INDEX idx_session_sequence ON interactions(session_id, sequence_number);
```

**Key Operations**
- Atomic transaction support for recording
- Efficient query patterns for mock matching
- Sequence number management per endpoint

### 3.4 Mock Engine

**Request Matching Algorithm**
1. Parse incoming request
2. Extract matching criteria (method, endpoint, headers)
3. Query database for matching interactions
4. Apply matching rules (exact, pattern, fuzzy)
5. Return matched response or 404

**Matching Strategies**
- Exact match: URL, method, headers, body
- Pattern match: Regex support for dynamic URLs
- Sequence-aware: Return responses in recorded order
- Fallback options: Default responses for unmatched requests

### 3.5 Export/Import Manager

**Export Format (JSON)**
```json
{
  "version": "1.0",
  "session": {
    "name": "api-testing-session",
    "created_at": "2024-01-15T10:30:00Z",
    "description": "Production API testing session"
  },
  "interactions": [
    {
      "request_id": "req_123456",
      "protocol": "REST",
      "method": "POST",
      "endpoint": "/api/v1/users",
      "request": {
        "headers": {
          "Content-Type": "application/json",
          "Authorization": "Bearer [REDACTED]"
        },
        "body": {
          "name": "John Doe",
          "email": "john@example.com"
        }
      },
      "response": {
        "status": 201,
        "headers": {
          "Content-Type": "application/json"
        },
        "body": {
          "id": 12345,
          "name": "John Doe",
          "email": "john@example.com"
        }
      },
      "timestamp": "2024-01-15T10:30:15Z",
      "sequence_number": 1
    }
  ]
}
```

**Features**
- Sensitive data redaction options
- Compression support for large datasets
- Validation on import
- Merge strategies for existing data

## 4. Configuration Design

### 4.1 Configuration File Format (YAML)

```yaml
proxy:
  mode: "record" # record | mock
  target_host: "api.example.com"
  target_port: 443
  listen_host: "0.0.0.0"
  listen_port: 8080
  protocol: "https"
  
database:
  path: "./recordings.db"
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
    status: 404
    body: {"error": "Recording not found"}
    
grpc:
  proto_paths:
    - "./protos"
  reflection_enabled: true
  
export:
  format: "json"
  pretty_print: true
  compress: false
```

### 4.2 Command Line Interface

```bash
# Start in proxy recording mode
proxy-intercept --mode record --config config.yaml

# Start in mock server mode
proxy-intercept --mode mock --config config.yaml

# Export session
proxy-intercept export --session "api-testing" --output session.json

# Import session
proxy-intercept import --input session.json --merge-strategy replace

# List recorded sessions
proxy-intercept list-sessions

# Clear specific session
proxy-intercept clear --session "api-testing"
```

## 5. Security Considerations

### 5.1 Data Protection
- Encryption at rest for sensitive data in SQLite
- Configurable redaction rules for sensitive headers/data
- SSL/TLS certificate validation options

### 5.2 Access Control
- Optional authentication for proxy access
- Read-only mode for production environments
- Audit logging for all operations

## 6. Performance Considerations

### 6.1 Optimization Strategies
- Connection pooling for upstream servers
- Async I/O for high throughput
- In-memory caching for frequently accessed mock data
- Database query optimization with proper indexing

### 6.2 Resource Management
- Configurable memory limits
- Database size management with rotation options
- Request/response size limits

## 7. Deployment Architecture

### 7.1 Standalone Binary
- Single executable with embedded SQLite
- Cross-platform support (Linux, macOS, Windows)
- Minimal dependencies

### 7.2 Container Deployment
```dockerfile
FROM alpine:latest
COPY proxy-intercept /usr/local/bin/
EXPOSE 8080
CMD ["proxy-intercept", "--config", "/config/config.yaml"]
```

### 7.3 CI/CD Integration
```yaml
# Example GitHub Actions workflow
- name: Import API Mocks
  run: |
    proxy-intercept import --input ./tests/mocks/api-session.json
    
- name: Run Integration Tests
  run: |
    proxy-intercept --mode mock --config mock-config.yaml &
    npm test
```

## 8. Extensibility

### 8.1 Plugin Architecture
- Request/response transformation plugins
- Custom matching algorithms
- Additional protocol support
- External storage backends

### 8.2 Integration Points
- Webhook notifications for recorded interactions
- Metrics export (Prometheus format)
- OpenTelemetry tracing support

## 9. Error Handling

### 9.1 Failure Scenarios
- Target server unavailable: Configurable retry with circuit breaker
- Database corruption: Automatic backup and recovery
- Invalid requests: Detailed error logging and client notification

### 9.2 Monitoring
- Health check endpoint
- Metrics collection (requests/sec, latency, errors)
- Structured logging with correlation IDs

## 10. Future Enhancements

1. **Web UI**: Browser-based interface for viewing/editing recordings
2. **Distributed Mode**: Multiple proxy instances with shared storage
3. **Smart Matching**: ML-based request matching for complex scenarios
4. **Performance Testing**: Load generation from recorded sessions
5. **Protocol Support**: WebSocket, GraphQL subscription support

## 11. Summary

This design provides a robust, extensible solution for API interaction recording and mocking. The architecture supports both REST and gRPC protocols, provides reliable storage with SQLite, and integrates seamlessly with modern CI/CD workflows through JSON export/import functionality. The modular design allows for future enhancements while maintaining a simple, single-binary deployment model.