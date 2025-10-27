# Streaming SSE Quick Start Guide

This guide will help you quickly get started with capturing and replaying Server-Sent Events (SSE) streaming responses, such as those from LLM APIs like Anthropic's Claude.

## Quick Start

### 1. Configure Streaming

Create a config file with `enable_streaming: true`:

```yaml
mode: record
proxies:
  anthropic:
    target_host: api.anthropic.com
    target_port: 443
    protocol: https
    session_name: streaming-demo
    enable_streaming: true  # Required for SSE streaming
```

### 2. Start Mimic in Record Mode

```bash
# Using the example streaming config
mimic web --config config-streaming-example.yaml

# Or use your own config with enable_streaming: true
mimic web --config my-streaming-config.yaml
```

### 3. Make a Streaming Request

```bash
# Example: Anthropic Claude API with streaming
curl http://localhost:8080/proxy/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: $ANTHROPIC_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Tell me a short story"}
    ],
    "stream": true
  }'
```

You should see:
- The streaming response in your terminal
- Mimic logs showing "Streaming enabled - handling SSE response"
- Chunks being captured and stored

### 4. Verify Capture

Check that the stream was captured:

```bash
# View sessions
mimic web --config config-streaming-example.yaml

# Open http://localhost:8080 in your browser
# Navigate to the session and view the recorded interaction
```

Or query the database directly:

```bash
sqlite3 ~/.mimic/streaming-recordings.db

# List streaming interactions
SELECT id, method, endpoint, is_streaming
FROM interactions
WHERE is_streaming = 1;

# Count chunks for an interaction (replace 1 with your interaction ID)
SELECT COUNT(*) as chunks, SUM(LENGTH(data)) as total_bytes
FROM stream_chunks
WHERE interaction_id = 1;
```

### 5. Mock Mode - Replay the Stream

```bash
# Start in mock mode
mimic web --config config-streaming-example.yaml --mode mock

# Make the same request (without the API key if you want)
curl http://localhost:8080/proxy/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 1024,
    "messages": [
      {"role": "user", "content": "Tell me a short story"}
    ],
    "stream": true
  }'
```

The captured stream will be replayed instantly!

### 6. Replay Mode - Validate Against Live API

```bash
# Replay and validate
mimic replay \
  --session anthropic-streaming \
  --target-host api.anthropic.com \
  --target-port 443 \
  --protocol https \
  --matching-strategy fuzzy \
  --timeout-seconds 120
```

## How It Works

### Detection
When `enable_streaming: true` is configured, Mimic detects SSE responses by checking for:
```
Content-Type: text/event-stream
```

The streaming flag must be enabled in your proxy configuration for SSE detection to work.

### Capture Process
1. Request forwarded to target API
2. Response detected as streaming
3. Each SSE chunk captured with timing:
   ```
   event: message_start
   data: {...}

   event: content_block_delta
   data: {...}
   ```
4. Chunks stored in `stream_chunks` table
5. Stream forwarded to client in real-time

### Storage
- **interactions table**: Stores request/response metadata with `is_streaming = 1`
- **stream_chunks table**: Stores individual chunks with:
  - `chunk_index`: Order of the chunk
  - `data`: Raw chunk data
  - `timestamp`: When chunk was received
  - `time_delta`: Milliseconds since previous chunk

### Replay
- **Mock mode**: Replays all chunks instantly by default (configurable via `respect_streaming_timing`)
- **Replay mode**: Validates chunks against live API response

## Advanced Usage

### Capture Multiple Streams

```bash
# Session per conversation
curl http://localhost:8080/proxy/anthropic/v1/messages \
  -H "X-Mimic-Session: conversation-1" \
  -H "Content-Type: application/json" \
  ...
```

### Export Streaming Sessions

```bash
# Export a streaming session
mimic export --session anthropic-streaming --output streaming.json
```

### Replay with Timing

To replay streaming chunks with their original timing in mock mode, set the `respect_streaming_timing` option in your config:

```yaml
mock:
  matching_strategy: exact
  sequence_mode: ordered
  respect_streaming_timing: true  # false (default) for immediate replay, true to respect original timing
  not_found_response:
    status: 404
    body:
      error: "Recording not found"
```

When `respect_streaming_timing` is `true`, mimic will replay each SSE chunk with the same time delays that were captured during recording. When `false` (default), all chunks are replayed immediately.

### Filter by Streaming

```sql
-- Get all streaming interactions
SELECT * FROM interactions WHERE is_streaming = 1;

-- Get streaming interactions with chunk counts
SELECT
  i.id,
  i.endpoint,
  i.method,
  COUNT(sc.id) as chunk_count,
  SUM(LENGTH(sc.data)) as total_bytes,
  i.timestamp
FROM interactions i
LEFT JOIN stream_chunks sc ON i.id = sc.interaction_id
WHERE i.is_streaming = 1
GROUP BY i.id
ORDER BY i.timestamp DESC;
```
