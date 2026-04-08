# Shelf

A production-grade, persistent key-value store with a lightweight observability stack. Written in Go, zero external dependencies.

## Components

Shelf is three independent binaries that work together:

| Binary | Purpose | RAM | CPU |
|---|---|---|---|
| **server** | Persistent KV store with HTTP API | ~10 MB | < 0.1 cores |
| **collector** | Scrapes metrics → append-only log | < 5 MB | < 0.01 cores |
| **query** | HTTP query API for metrics.log | < 20 MB | < 0.1 cores |

**Total system: < 35 MB RAM, < 0.25 CPU cores.** vs Prometheus at 1-4 GB RAM, 1-2 cores.

## Quick Start

### KV Store Only

```bash
go build -o shelf ./cmd/server
./shelf -addr :8080 -data ./shelf.db -shards 16 -snapshot 10000

curl -X PUT localhost:8080/keys/name -d '{"value":"QWxpY2U="}'
curl localhost:8080/keys/name
```

### Full Observability Pipeline

```bash
# 1. Build all binaries
go build ./cmd/server ./cmd/collector ./cmd/query

# 2. Start the KV store
./cmd/server -addr :8080 -data ./shelf.db -snapshot 0 &

# 3. Generate some traffic
curl -X PUT localhost:8080/keys/name -d '{"value":"QWxpY2U="}'
curl localhost:8080/keys/name

# 4. Start the collector (scrapes every 5s)
echo '{"targets":[{"name":"shelf","url":"http://localhost:8080/metrics","interval_seconds":5}]}' > targets.json
./cmd/collector targets.json &

# 5. Start the query API
./cmd/query metrics.log :9090 &

# 6. Query your metrics
curl 'localhost:9090/query?q=shelf_store_size'
curl 'localhost:9090/query?q=sum(shelf_http_requests_total)'
curl 'localhost:9090/query?q=rate(shelf_http_request_duration_seconds_count[5m])'
```

### As a Go Library

```go
package main

import (
    "fmt"
    "shelf"
)

func main() {
    store, err := shelf.Open("./data", 16, 10000)
    if err != nil {
        panic(err)
    }
    defer store.Close()

    store.Set("name", []byte("Alice"))
    value, found := store.Get("name")
    fmt.Println(string(value)) // "Alice"
}
```

---

## KV Store

### HTTP API

| Method | Path | Description |
|---|---|---|
| `PUT` | `/keys/{key}` | Set a key-value pair |
| `GET` | `/keys/{key}` | Get a value by key |
| `DELETE` | `/keys/{key}` | Delete a key |
| `GET` | `/keys` | List all keys |
| `GET` | `/size` | Get entry count |
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus-format metrics |

All values are base64-encoded in JSON to support binary data.

### Server Flags

| Flag | Default | Description |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-data` | `./shelf.db` | Data directory |
| `-shards` | `16` | Number of hash table shards |
| `-snapshot` | `10000` | Auto-snapshot threshold (0 to disable) |
| `-sync-writes` | `false` | `fsync` WAL on every write (stronger durability, lower throughput) |
| `-max-value-bytes` | `1048576` | Maximum decoded value size accepted by `PUT /keys/{key}` |
| `-read-timeout` | `5s` | HTTP read timeout |
| `-write-timeout` | `10s` | HTTP write timeout |
| `-idle-timeout` | `60s` | HTTP keep-alive idle timeout |

### Storage Engine

```
data/
  wal.log        (append-only write-ahead log)
  snapshot.db    (compact point-in-time state dump)
```

**Write path**: Every `Set`/`Delete` appends a binary entry to `wal.log` (with CRC32 checksum), then applies to the in-memory hash table. By default, WAL is synced on close/snapshot for lower write latency. Enable `-sync-writes` for per-write `fsync` durability.

**Recovery**: On `Open()`, shelf loads `snapshot.db` (if valid), then replays `wal.log` entries on top. Corrupt snapshots are detected via CRC and discarded. Partial WAL entries (from crashes) are truncated.

**Snapshots**: When writes exceed `snapshotThreshold`, shelf creates an atomic snapshot (temp file + rename), truncates the WAL, and resets the counter.

---

## Observability

### Architecture

```
┌─────────────┐    pull /metrics     ┌──────────────┐    append     ┌─────────────┐     query      ┌──────────────┐
│   shelf     │ ◄────────────────── │  collector   │ ───────────►  │ metrics.log │ ◄───────────  │  query API   │
│  (KV store) │    every N seconds   │  (binary #2) │               │ (text file) │               │  (binary #3) │
└─────────────┘                      └──────────────┘               └─────────────┘               └──────────────┘
```

### Metrics Endpoint

Shelf exposes `/metrics` in Prometheus-compatible text format with:

| Metric | Type | Description |
|---|---|---|
| `shelf_http_requests_total` | Counter | Total HTTP requests |
| `shelf_http_request_duration_seconds` | Histogram | Request latency |
| `shelf_store_size` | Gauge | Number of entries in the store |

### Collector

Scrapes `/metrics` from configured targets and appends to an append-only log file.

**Config (`targets.json`):**

```json
{
    "output": "metrics.log",
    "max_size_mb": 50,
    "targets": [
        {
            "name": "shelf",
            "url": "http://localhost:8080/metrics",
            "interval_seconds": 15
        }
    ]
}
```

*Note: Set `max_size_mb` to limit disk usage. When the log exceeds this size, it will be automatically truncated. Set to 0 to disable (append forever).*

**Usage:**

```bash
./cmd/collector targets.json          # use config file
./cmd/collector /path/to/config.json  # custom config path
```

**Log format:**

```
1712345678 shelf_http_requests_total 42
1712345678 shelf_http_request_duration_seconds_bucket{le="0.001"} 5
1712345678 shelf_store_size 100
```

One line per metric: `timestamp name{labels} value`. Append-only, no index, no compaction.

### Query API

Reads `metrics.log` and serves queries via HTTP.

At startup, query builds an in-memory index of parsed entries and incrementally tails new appended data before each request. This avoids reparsing the full log file on every query while keeping the append-only file format unchanged.

**Usage:**

```bash
./cmd/query                                 # default: metrics.log on :9090, 24h retention
./cmd/query metrics.log                     # custom log file
./cmd/query -retention 72h metrics.log :9090 # custom retention limit, custom log and port
```

*Note: `query` stores all log entries in memory. Setting `-retention` to e.g. `24h` strictly bounds its RAM usage by evicting metrics older than 24 hours. Disable eviction with `-retention 0`.*

**Query DSL:**

| Query | Description |
|---|---|
| `metric_name` | Raw data points |
| `metric_name[5m]` | Data from last 5 minutes |
| `sum(metric_name)` | Sum of all values |
| `avg(metric_name)` | Average of all values |
| `min(metric_name)` | Minimum value |
| `max(metric_name)` | Maximum value |
| `count(metric_name)` | Number of data points |
| `rate(metric_name[5m])` | Per-second rate over window |

**Examples:**

```bash
curl 'localhost:9090/query?q=shelf_store_size'
# {"metric":"shelf_store_size","data":[{"timestamp":1712345678,"value":100}]}

curl 'localhost:9090/query?q=sum(shelf_http_requests_total)'
# {"metric":"shelf_http_requests_total","result":42}

curl 'localhost:9090/query?q=rate(shelf_http_request_duration_seconds_count[5m])'
# {"metric":"shelf_http_request_duration_seconds_count","result":0.8}
```

**Response format:**

- Raw queries: `{"metric":"...","data":[{"timestamp":...,"value":...}]}`
- Aggregation queries: `{"metric":"...","result":...}`
- Errors: `{"error":"..."}`

### Resource Comparison

| System | RAM | CPU | Features |
|---|---|---|---|
| **Shelf observability** | < 35 MB | < 0.25 cores | Metrics, simple queries |
| **Prometheus** | 1-4 GB | 1-2 cores | Full PromQL, service discovery, alerting |

Shelf's observability stack is designed for small deployments where Prometheus is overkill. It trades advanced features (PromQL, service discovery, alerting) for minimal resource usage and operational simplicity.

---

## Performance

Benchmarks on AMD Ryzen 5 5500U (single-threaded):

| Operation | Latency | Notes |
|---|---|---|
| HashTable Get | ~580 ns/op | In-memory only |
| HashTable Set | ~4.7 µs/op | In-memory + resize |
| Store Get | ~580 ns/op | In-memory (no I/O) |
| Store Set | ~4.7 µs/op | WAL write + memory |
| HTTP GET | ~6.3 µs/op | JSON + base64 overhead |
| HTTP PUT | ~11.5 µs/op | JSON + base64 + WAL |

Concurrent throughput scales with shard count. Reads parallelize well; writes are serialized by the WAL mutex.

## Project Structure

```
shelf/
  hashmap.go           # Generic sharded hash table
  hashmap_test.go      # Hash table tests + benchmarks
  storage.go           # WAL persistence + snapshots
  storage_test.go      # Storage tests + benchmarks
  metrics.go           # Counter, Gauge, Histogram, Registry
  metrics_test.go      # Metrics tests
  integration_test.sh  # End-to-end pipeline test
  cmd/
    server/
      main.go          # HTTP REST server + /metrics
      main_test.go     # Server tests + benchmarks
    collector/
      main.go          # Metrics scraper → metrics.log
      main_test.go     # Collector tests
    query/
      main.go          # Query API for metrics.log
      main_test.go     # Query tests
    demo/
      main.go          # Quick demo
```

## Development

```bash
make build           # Build all binaries
make test            # Run all tests with race detector
make bench           # Run all benchmarks
make lint            # Run go vet
make clean           # Remove build artifacts
make run-server      # Start the KV store server
make run-collector   # Start the metrics collector
make run-query       # Start the query API
make run-demo        # Run the quick demo
make help            # Show all targets
```

### Try the CLI Dashboard (`cmd/top`)

`shelf` comes with an `htop`-like visualization tool to monitor QPS, memory, and store size in real-time.

**If running natively:**
```bash
make build
./bin/server -addr :8080 -data ./shelf.db &
./bin/top -url http://localhost:8080/metrics
```

**If running via Docker Compose:**
```bash
docker compose up -d --build
docker compose run --rm top
```

## Testing

```bash
# All tests with race detector
make test

# Integration test (full pipeline)
bash integration_test.sh

# Benchmarks
go test -bench=. ./...
```

## License

MIT
