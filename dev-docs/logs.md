# Service Logs

## Overview

Asty provides **real-time log streaming** via Server-Sent Events (SSE). Logs are collected from service processes running on agents and streamed live to the web UI.

## Architecture

```
UI ←── SSE ←── API Server ←── NATS (Pub/Sub) ←── Agent ←── Process Log Files
```

1. **Process (agent)**: Each service process writes stdout/stderr to `{workdir}/logs/{service_name}.log`
2. **Agent**: Continuously tails log files and publishes new lines to NATS subject `asty.v1.agent.{node_id}.logs.{service_name}`
3. **Server**: Subscribes to NATS log stream and forwards via SSE to HTTP clients
4. **UI**: Opens SSE connection to `/api/v1/logs/allocation/{id}?follow=true` and displays live logs

## Implementation

### Agent Side

- **Process**: Redirects stdout/stderr to `{workdir}/logs/{service_name}.log` (see `process.go:setupLogs`)
- **Log tailing**: `Process.TailLogs(ctx, lines chan)` continuously reads new log lines from file
- **Log streaming**: `Agent.streamProcessLogs()` publishes log lines to NATS in real-time
- **NATS subject**: `asty.v1.agent.{node_id}.logs.{service_name}`
- **Initial logs**: `Agent.handleLogsCommand()` provides last N lines on request for bootstrap

### Server Side

- **API endpoint**: `GET /api/v1/logs/allocation/{id}?follow=true&lines=100`
- **Query params**:
  - `follow=true` — Enable SSE streaming (default: false, returns JSON)
  - `lines=100` — Number of initial lines to return (default: 100)
- **Non-streaming mode** (`follow=false`): Returns JSON with initial logs via NATS Request-Reply
- **Streaming mode** (`follow=true`):
  1. Requests initial logs via NATS `asty.v1.agent.{node_id}.cmd`
  2. Subscribes to NATS `asty.v1.agent.{node_id}.logs.{service_name}`
  3. Forwards log events to client via SSE
- **SSE format**:
  ```
  data: {"line": "log message", "timestamp": 1234567890}
  
  data: {"line": "another message", "timestamp": 1234567891}
  ```

### Commands Protocol

**Initial logs request** (Server → Agent via NATS Request-Reply):
```json
{
  "type": "logs",
  "data": {
    "service_name": "xhttp",
    "lines": 100,
    "follow": false
  }
}
```

**Response** (Agent → Server):
```json
{
  "success": true,
  "logs": ["line1", "line2", "line3"],
  "error": ""
}
```

**Real-time streaming** (Agent → Server via NATS Pub/Sub):
- Subject: `asty.v1.agent.{node_id}.logs.{service_name}`
- Message:
  ```json
  {"line": "log message", "timestamp": 1234567890}
  ```

## Usage

### API

```bash
# Get last 100 lines (JSON response)
curl http://localhost:8080/api/v1/logs/allocation/{alloc-id}

# Get last 50 lines
curl http://localhost:8080/api/v1/logs/allocation/{alloc-id}?lines=50

# Stream logs in real-time (SSE)
curl -N http://localhost:8080/api/v1/logs/allocation/{alloc-id}?follow=true&lines=50
```

### Web UI

1. Navigate to Dashboard
2. Click on a node
3. Click on a service allocation
4. Select "Logs" tab
5. Logs stream in real-time with "Live" badge indicator
6. Click "Clear" to clear the log buffer
7. Auto-scrolls to bottom as new logs arrive

## Log Rotation

Log files are currently **not rotated**. Future implementation should:
- Implement log rotation in `process.go`
- Add configuration via `.asty` file `logs.max_files` and `logs.max_file_size`
- Consider using system logrotate or built-in rotation

## Limitations

- **No node-level logs**: `/api/v1/logs/node/{id}` returns placeholder — agent logs go to system logger (journalctl, docker logs)
- **No log rotation**: Log files grow unbounded (TODO: implement rotation via `logs.max_files` and `logs.max_file_size`)
- **No search/filter**: UI shows raw logs without search or filtering capability
- **Memory buffering**: UI keeps all received logs in memory (consider implementing virtual scrolling for very long sessions)

## Future Enhancements

1. **Structured logging**: Parse zerolog JSON output and display formatted logs in UI (timestamp, level, fields in table format)
2. **Log aggregation**: Store logs in centralized storage (S3, Loki, Elasticsearch) for long-term retention
3. **Search & filter**: Add full-text search, regex filter, log level filter in UI
4. **Node logs**: Collect agent logs via systemd journal or container runtime
5. **Download**: Allow downloading logs as file from UI
6. **Compression**: Compress log events before sending over NATS to reduce bandwidth
7. **Rate limiting**: Throttle log streaming if too many lines per second (prevent UI/network overload)

## Testing

```bash
# Run logs unit tests
go test -v ./internal/platform/asty -run TestProcessLogs
go test -v ./internal/platform/asty -run TestSplitLines

# Integration test (requires running agent + NATS)
# 1. Start NATS server
nats-server

# 2. Start agent in one terminal
A_MODE=agent A_NODE_ID=test-node ./asty

# 3. Start server in another terminal
A_MODE=server ./asty

# 4. Stream logs via SSE
curl -N http://localhost:8080/api/v1/logs/allocation/{alloc-id}?follow=true

# 5. Open UI and navigate to service → Logs tab
open http://localhost:8080
```

## Log Format

Service logs are written as plain text (stdout/stderr). For services using zerolog (like xhttp, xauth):

```json
{"level":"info","service":"xhttp","method":"GET","path":"/health","time":"2026-05-05T00:00:00Z","message":"request completed"}
```

Future UI enhancement: parse JSON logs and display in structured table format.

## Performance Considerations

- **Polling interval**: Agent tails logs every 100ms (`process.go:TailLogs`)
- **Buffer size**: Log line channel has 100-line buffer
- **NATS overhead**: Each log line is published as separate NATS message (~200 bytes overhead)
- **Network**: SSE uses HTTP chunked transfer for efficient streaming
- **Reconnection**: UI auto-reconnects after 5 seconds if SSE connection drops

For high-volume services (>1000 lines/sec), consider:
- Batching multiple lines into single NATS message
- Sampling/filtering logs at agent before publishing
- Using dedicated log aggregation system (Loki, Fluentd)
