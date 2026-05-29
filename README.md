# GoExec — Distributed Code Execution Engine

A production-grade sandboxed code execution engine built in Go. Submissions run inside isolated Docker containers with configurable CPU, memory, and time limits. Results are delivered via REST polling or streamed in real time over WebSockets.

> Built as a backend system similar to Codeforces / LeetCode  judge infrastructure.

---

## Architecture

```
Client
  │
  ▼
POST /jobs  ──►  Handler (validates + enqueues)  ──►  job_id returned immediately
                        │
                        ▼
               ┌─────────────────────┐
               │  Worker Pool        │
               │  (10 goroutines)    │
               │                     │
               │  jobs channel ───►  goroutine 0
               │                     goroutine 1
               │                     ...
               │                     goroutine 9
               └────────┬────────────┘
                        │
                 cache check (SHA-256)
                /                    \
           HIT                       MISS
            │                          │
        return cached              Docker SDK
        result instantly               │
                          ┌────────────▼──────────────┐
                          │  Isolated Container        │
                          │  --cpus       (CFS quota)  │
                          │  --memory     (hard limit) │
                          │  --pids-limit (fork bomb)  │
                          │  --network=none            │
                          │  --cap-drop ALL            │
                          │  --security-opt no-new-priv│
                          └────────────┬──────────────┘
                                       │
                                 stdout / stderr
                                       │
                              classify verdict
                                       │
                          ┌────────────▼──────────────┐
                          │  AC / TLE / MLE / RE / CE  │
                          └────────────┬──────────────┘
                                       │
                              cache.Set (AC only)
                                       │
                            deliver to client
                          (REST poll or WebSocket)
```

---

## Features

- **Docker sandboxing** — every submission runs in a fresh isolated container. No shared state, no network, no privilege escalation.
- **Resource enforcement** — configurable CPU quota (CFS), memory hard limit (swap disabled), pids limit to prevent fork bombs.
- **Goroutine worker pool** — fixed pool of N goroutines pulling from a buffered channel. Natural backpressure: returns HTTP 503 when queue is full instead of spawning unbounded goroutines.
- **Verdict classification** — per-language pattern matching correctly distinguishes Compile Error from Runtime Error (e.g. Python's `ZeroDivisionError` is Runtime Error, not Compile Error).
- **Verdict cache** — SHA-256 keyed on (language + code + stdin). Only `Accepted` results are cached. Errors always re-execute so the user can fix and retry.
- **WebSocket streaming** — real-time stdout/stderr streaming per job over `/ws/jobs/{id}`.
- **Admin metrics API** — P50/P95/P99 latency, verdict breakdown, active workers, queue length, cache hit rate.
- **Graceful shutdown** — drains in-flight jobs before exit.
- **Pre-warmed build cache** — Go sandbox image bakes stdlib compilation artifacts at build time, cutting cold-start from ~7s to ~2s.

---

## Supported Languages

| Language   | Image                          | Run strategy              |
|------------|-------------------------------|---------------------------|
| Go         | `goexec-sandbox-go:latest`    | `go build` → run binary   |
| Python 3   | `goexec-sandbox-python:latest`| `python3 main.py`         |
| C++ (GCC)  | `goexec-sandbox-cpp:latest`   | `g++ -O2` → run binary    |
| Java       | `goexec-sandbox-java:latest`  | `javac` → `java`          |

---

## Quick Start

### Prerequisites

- Go 1.21+
- Docker Desktop running

### 1. Clone the repo

```bash
git clone https://github.com/BasavarajBankolli/goexec.git
cd goexec
```

### 2. Install dependencies

```bash
go mod tidy
```

### 3. Build sandbox images

```bash
# Build all language sandbox images (do this once)
docker build -f Dockerfile.sandbox.go     -t goexec-sandbox-go:latest     .
docker build -f Dockerfile.sandbox.python -t goexec-sandbox-python:latest .
docker build -f Dockerfile.sandbox.cpp    -t goexec-sandbox-cpp:latest    .
docker build -f Dockerfile.sandbox.java   -t goexec-sandbox-java:latest   .
```

> The Go image pre-warms the build cache during `docker build` — this takes ~2 min but only needs to be done once.

### 4. Start the server

```bash
go run ./cmd/server
```

```
GoExec listening on http://0.0.0.0:8080
  Workers : 10
  Queue   : 100
  Timeout : 15s
  Cache   : TTL=10m0s
```

---

## API Reference

### POST /jobs — Submit code

```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{
    "language":   "go",
    "code":       "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hello\")}",
    "timeout_ms": 15000,
    "memory_mb":  128
  }'
```

**Response 202:**
```json
{
  "job_id": "4037a0d452cb06a1ed901f0d4ec3b453",
  "message": "job enqueued; connect to /jobs/4037a0d452cb06a1ed901f0d4ec3b453 for live output"
}
```

---

### GET /jobs/{id} — Poll for result

```bash
curl http://localhost:8080/jobs/4037a0d452cb06a1ed901f0d4ec3b453
```

**Response 200:**
```json
{
  "job_id":      "4037a0d452cb06a1ed901f0d4ec3b453",
  "verdict":     "Accepted",
  "stdout":      "hello\n",
  "stderr":      "",
  "exit_code":   0,
  "duration_ms": 2832777500,
  "cached":      false
}
```

---

### GET /jobs/{id} — WebSocket stream

Connect with any WebSocket client. Receives a stream of JSON events:

```json
{ "type": "stdout",  "payload": "hello" }
{ "type": "verdict", "result": { "verdict": "Accepted", ... } }
```

Event types: `stdout`, `stderr`, `verdict`, `error`

---

### GET /admin/metrics — Engine statistics

```bash
curl http://localhost:8080/admin/metrics \
  -H "Authorization: Bearer secret-token"
```

**Response 200:**
```json
{
  "total_queued":       24,
  "total_completed":    24,
  "total_cache_hits":   6,
  "cache_hit_rate_pct": 25.0,
  "verdicts": {
    "Accepted":             18,
    "Runtime Error":         3,
    "Time Limit Exceeded":   2,
    "Compile Error":         1
  },
  "latency_p50_ms":  1400,
  "latency_p95_ms":  3800,
  "latency_p99_ms":  6200,
  "active_workers":  0,
  "queue_len":       0,
  "cache_size":      12
}
```

---

### GET /health — Liveness probe

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

---

## Verdicts

| Verdict                  | Meaning                                              |
|--------------------------|------------------------------------------------------|
| `Accepted`               | Code ran successfully, exit code 0                   |
| `Time Limit Exceeded`    | Wall clock exceeded `timeout_ms`; container killed   |
| `Memory Limit Exceeded`  | Container killed by OOM killer                       |
| `Runtime Error`          | Non-zero exit code (crash, exception, panic)         |
| `Compile Error`          | Compiler/parser failure before execution             |
| `System Error`           | Docker daemon unreachable or internal engine failure |

### Verdict classification logic

The classifier uses **per-language patterns** to correctly separate compile errors from runtime exceptions:

| Language | Compile Error signals                        | Everything else       |
|----------|----------------------------------------------|-----------------------|
| Python   | `SyntaxError:`, `IndentationError:`          | Runtime Error         |
| C++      | `main.cpp:N:N: error:` (gcc format)          | Runtime Error         |
| Go       | `./main.go:N:N:` (compiler format)           | Runtime Error         |
| Java     | `Main.java:N: error:` (javac format)         | Runtime Error         |

> A naive `contains("error:")` check misclassifies Python runtime exceptions like `ZeroDivisionError:` as Compile Errors because the word "error" appears in the exception name itself.

---

## Configuration

All settings are read from environment variables:

| Variable             | Default        | Description                              |
|----------------------|----------------|------------------------------------------|
| `PORT`               | `8080`         | HTTP listen port                         |
| `WORKER_COUNT`       | `10`           | Goroutines in the worker pool            |
| `JOB_QUEUE_SIZE`     | `100`          | Buffered job channel capacity            |
| `DEFAULT_CPU_QUOTA`  | `1.0`          | Fractional CPUs per container            |
| `DEFAULT_MEMORY_MB`  | `128`          | Memory limit per container (MB)          |
| `DEFAULT_TIMEOUT`    | `15s`          | Wall-clock limit per job                 |
| `CACHE_TTL`          | `10m`          | How long to keep cached verdicts         |
| `METRICS_TOKEN`      | `secret-token` | Bearer token for `/admin/metrics`        |

---

## Security Model

Each container runs with:

| Control                        | Value / Effect                              |
|--------------------------------|---------------------------------------------|
| `--network=none`               | No outbound internet access                 |
| `--cap-drop ALL`               | No Linux capabilities                       |
| `--security-opt no-new-privileges` | Cannot escalate privileges              |
| `--pids-limit`                 | Go: 512 / Python & C++: 256 / Java: 512    |
| Memory swap disabled           | `MemorySwap == Memory` — no swap escape     |
| Non-root user                  | `sandbox` user inside every container       |

---

## Project Structure

```
goexec/
├── cmd/server/main.go              # Entrypoint — wires all components, graceful shutdown
├── api/types.go                    # Shared types: Job, Result, Verdict, WSEvent
├── config/config.go                # Env-var config with defaults
├── internal/
│   ├── executor/executor.go        # Docker SDK sandboxing + verdict classification
│   ├── worker/pool.go              # Goroutine pool, job queue, result store
│   ├── cache/cache.go              # SHA-256 keyed TTL verdict cache
│   ├── metrics/metrics.go          # Atomic counters + P50/P95/P99 latency
│   ├── handler/handler.go          # HTTP REST handlers
│   └── ws/hub.go                   # WebSocket hub for real-time streaming
├── Dockerfile                      # Multi-stage build → distroless image
├── Dockerfile.sandbox.go           # Go sandbox (pre-warms build cache)
├── Dockerfile.sandbox.python       # Python 3 sandbox
├── Dockerfile.sandbox.cpp          # C++ / GCC sandbox
├── docker-compose.yml
└── build-sandboxes.sh
```

---

## Running Tests

```bash
go test ./...
```

Tests cover:
- Cache hit / miss / TTL expiry / key isolation
- Metrics counters and P50/P95/P99 percentile calculation

---

## Benchmarks

Tested on a 4-core machine with Docker Desktop:

| Metric              | Value       |
|---------------------|-------------|
| Concurrent jobs     | 200+        |
| P50 latency         | ~1.4 s      |
| P95 latency         | ~3.8 s      |
| Cache hit latency   | < 1 ms      |
| Python execution    | ~500 ms     |
| Go execution        | ~2.5 s      |
| C++ execution       | ~2.0 s      |
| Java execution      | ~2.5 s      |

> Dominant cost is Docker container startup (~300–400 ms). A warm container pool would cut P50 latency significantly.

---

## Known Limitations & Future Improvements

| Limitation | Production Fix |
|---|---|
| Fresh container per job (~400ms overhead) | Warm container pool — pre-start containers, inject code |
| Compile time counts against TLE | Separate compile phase from execution timeout |
| In-memory metrics reset on restart | Prometheus + Grafana or write to Redis |
| No deduplication of identical in-flight jobs | `singleflight` group to share one execution |
| Docker socket mounted (sibling containers) | gVisor or Firecracker for kernel-level isolation |

---

## Author

**Basavaraj Bankolli** — [github.com/BasavarajBankolli](https://github.com/BasavarajBankolli)
