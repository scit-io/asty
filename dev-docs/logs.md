# Service Logs

## Overview

Asty provides real-time access to service logs through its HTTP API. Logs are collected from service processes running on agents and made available through the web UI and API.

## Architecture

```
UI → API Server → NATS → Agent → Process Log Files
```

1. **Process (agent)**: Each service process writes stdout/stderr to a log file in `{workdir}/logs/{service_name}.log`
2. **Agent**: Responds to log requests via NATS subject `asty.v1.agent.{node_id}.cmd` with command type `logs`
3. **Server**: Forwards log requests from HTTP API to the appropriate agent via NATS
4. **UI**: Displays logs in the service detail page under the "Logs" tab

## Implementation

### Agent Side

- **Process**: Redirects stdout/stderr to `{workdir}/logs/{service_name}.log` (see `process.go:setupLogs`)
- **Log retrieval**: `Process.GetLogs(lines int)` reads log file and returns last N lines
- **NATS handler**: `Agent.handleLogsCommand()` processes log requests and responds with log data

### Server Side

- **API endpoint**: `GET /api/v1/logs/allocation/{id}?lines=100`
- **NATS request**: Sends `logs` command to agent via `asty.v1.agent.{node_id}.cmd`
- **Response format**:
  ```json
  {
    "allocation_id": "alloc-123",
    "service_name": "xhttp",
    "node_id": "node-1",
    "logs": ["line1", "line2", "line3"],
    "line_count": 3
  }
  ```

### Commands Protocol

**Request** (Server → Agent):
```json
{
  "type": "logs",
  "data": {
    "service_name": "xhttp",
    "lines": 100
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

## Usage

### API

```bash
# Get last 100 lines (default)
curl http://localhost:8080/api/v1/logs/allocation/{alloc-id}

# Get last 50 lines
curl http://localhost:8080/api/v1/logs/allocation/{alloc-id}?lines=50
```

### Web UI

1. Navigate to Dashboard
2. Click on a node
3. Click on a service allocation
4. Select "Logs" tab
5. Logs are displayed in a scrollable terminal-like view

## Log Rotation

Log files are currently **not rotated**. Future implementation should:
- Implement log rotation in `process.go`
- Add configuration via `.asty` file `logs.max_files` and `logs.max_file_size`
- Consider using system logrotate or built-in rotation

## Limitations

- **No streaming**: Logs are fetched on demand, not streamed in real-time (SSE streaming commented out in `api.go:handleLogsAllocation`)
- **No node-level logs**: `/api/v1/logs/node/{id}` returns placeholder — agent logs go to system logger (journalctl, docker logs)
- **Memory usage**: Large log files are read entirely into memory (TODO: implement efficient tail for large files)
- **No search/filter**: UI shows raw logs without search or filtering capability

## Future Enhancements

1. **Real-time streaming**: Implement SSE (Server-Sent Events) for live log tailing
2. **Structured logging**: Parse zerolog JSON output and display formatted logs in UI
3. **Log aggregation**: Store logs in centralized storage (S3, Loki) for long-term retention
4. **Search**: Add log search and filtering in UI
5. **Node logs**: Collect agent logs via systemd journal or container runtime
6. **Download**: Allow downloading logs as file from UI

## Testing

```bash
# Run logs unit tests
go test -v ./internal/platform/asty -run TestProcessLogs
go test -v ./internal/platform/asty -run TestSplitLines

# Integration test (requires running agent)
# 1. Start agent in one terminal
A_MODE=agent A_NODE_ID=test-node ./asty

# 2. Start server in another terminal
A_MODE=server ./asty

# 3. Request logs via API
curl http://localhost:8080/api/v1/logs/allocation/{alloc-id}
```

## Log Format

Service logs are written as plain text (stdout/stderr). For services using zerolog (like xhttp, xauth):

```json
{"level":"info","service":"xhttp","method":"GET","path":"/health","time":"2026-05-05T00:00:00Z","message":"request completed"}
```

Future UI enhancement: parse JSON logs and display in structured table format.
