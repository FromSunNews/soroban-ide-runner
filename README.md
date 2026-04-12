# Soroban Studio Backend

A production-ready backend system for a web-based Soroban (Rust) playground.

## Architecture

```
┌──────────────┐     POST /run      ┌──────────────┐
│              │ ──────────────────► │              │
│   Frontend   │                    │   Backend    │
│   (Browser)  │ ◄──────────────── │   (Go API)   │
│              │     WS /ws         │              │
└──────────────┘                    └──────┬───────┘
                                          │ docker exec
                                          ▼
                                   ┌──────────────┐
                                   │   Soroban    │
                                   │   Runner     │
                                   │   (Rust)     │
                                   └──────────────┘
```

## Quick Start

```bash
docker compose up --build
```

> ⚠️ First build takes 15-20 minutes (compiling soroban-cli from source).

## API

### POST /run

Submit files for compilation:

```bash
curl -X POST http://localhost:8080/run \
  -H "Content-Type: application/json" \
  -d '{
    "files": {
      "Cargo.toml": "[package]\nname = \"hello\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\nsoroban-sdk = \"20.0.0\"\n\n[lib]\ncrate-type = [\"cdylib\"]\n\n[profile.release]\nopt-level = \"z\"",
      "src/lib.rs": "use soroban_sdk::{contract, contractimpl, Env, Symbol, symbol_short};\n\n#[contract]\npub struct HelloContract;\n\n#[contractimpl]\nimpl HelloContract {\n    pub fn hello(env: Env, to: Symbol) -> Symbol {\n        symbol_short!(\"Hello\")\n    }\n}"
    }
  }'
```

Response:
```json
{ "session_id": "abc12345" }
```

### WS /ws?session_id=abc12345

Connect via WebSocket to receive real-time build output:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws?session_id=abc12345');
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  // msg.type: "stdout" | "stderr" | "info" | "error" | "done"
  // msg.content: the output text
  console.log(`[${msg.type}] ${msg.content}`);
};
```

### GET /health

Health check endpoint.

## Project Structure

```
soroban-studio-backend/
├── cmd/server/main.go           # Entry point
├── internal/
│   ├── model/types.go           # Shared data types
│   ├── session/manager.go       # WebSocket session management
│   ├── queue/worker.go          # Job queue with worker pool
│   ├── executor/docker.go       # Docker exec logic
│   ├── handler/
│   │   ├── run.go               # POST /run handler
│   │   └── websocket.go         # WebSocket handler
│   └── middleware/cors.go       # CORS middleware
├── docker/runner/Dockerfile     # Soroban runner image
├── Dockerfile                   # Backend image
├── docker-compose.yml           # Full stack orchestration
└── go.mod                       # Go module
```

## Configuration

| Variable          | Default          | Description                    |
|-------------------|------------------|--------------------------------|
| `PORT`            | `8080`           | HTTP server port               |
| `MAX_WORKERS`     | `3`              | Max concurrent build jobs      |
| `RUNNER_CONTAINER`| `soroban-runner`  | Docker container name          |
| `WORKSPACE_DIR`   | `/app/workspaces`| Shared workspace directory     |
