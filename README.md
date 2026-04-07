# Shelf

A production-grade, persistent key-value store written in Go. Shelf combines a concurrent sharded hash table with write-ahead log (WAL) persistence, automatic snapshot compaction, and an HTTP REST API.

## Features

- **Generic type-safe API** — Go generics with `HashTable[K comparable, V any]`
- **Concurrent** — Sharded `sync.RWMutex` for parallel reads and writes
- **Persistent** — WAL with CRC32 corruption detection and crash recovery
- **Auto-compaction** — Configurable snapshot threshold keeps WAL bounded
- **HTTP REST API** — JSON endpoints, graceful shutdown, request logging
- **Tested** — 66+ tests with race detector, comprehensive benchmarks
- **Zero dependencies** — Only the Go standard library

## Quick Start

### As a Standalone Server

```bash
# Build
go build -o shelf ./cmd/server

# Run
./shelf -addr :8080 -data ./shelf.db -shards 16 -snapshot 10000

# Use it
curl -X PUT localhost:8080/keys/name -d '{"value":"QWxpY2U="}'
curl localhost:8080/keys/name
curl -X DELETE localhost:8080/keys/name
curl localhost:8080/health
curl localhost:8080/size
curl localhost:8080/keys
```

### As a Go Library

```go
package main

import (
    "fmt"
    "shelf"
)

func main() {
    // Open a persistent store
    store, err := shelf.Open("./data", 16, 10000)
    if err != nil {
        panic(err)
    }
    defer store.Close()

    // Set and get values
    store.Set("name", []byte("Alice"))
    value, found := store.Get("name")
    fmt.Println(string(value)) // "Alice"

    // Delete
    store.Delete("name")
}
```

## HTTP API

### Endpoints

| Method | Path | Description |
|---|---|---|
| `PUT` | `/keys/{key}` | Set a key-value pair |
| `GET` | `/keys/{key}` | Get a value by key |
| `DELETE` | `/keys/{key}` | Delete a key |
| `GET` | `/keys` | List all keys |
| `GET` | `/size` | Get entry count |
| `GET` | `/health` | Health check |

### Examples

**Set a value**
```bash
curl -X PUT localhost:8080/keys/greeting -d '{"value":"SGVsbG8gV29ybGQ="}'
# Response: {"ok":true}
```

**Get a value**
```bash
curl localhost:8080/keys/greeting
# Response: {"value":"SGVsbG8gV29ybGQ=","found":true}
```

**Delete a key**
```bash
curl -X DELETE localhost:8080/keys/greeting
# Response: {"deleted":true}
```

**List all keys**
```bash
curl localhost:8080/keys
# Response: ["greeting","name","age"]
```

**Health check**
```bash
curl localhost:8080/health
# Response: {"status":"ok"}
```

All values are base64-encoded in JSON to support binary data.

### Error Responses

| Status | Meaning |
|---|---|
| `400` | Bad request (invalid JSON, missing key, bad base64) |
| `404` | Key not found |
| `405` | Method not allowed |
| `500` | Internal server error |

## Go Library API

### `shelf.Open(dir string, numShards int, snapshotThreshold int) (*Store, error)`

Opens or creates a persistent store at the given directory.

- `dir` — data directory (created if it doesn't exist)
- `numShards` — number of hash table shards (recommended: 16)
- `snapshotThreshold` — auto-snapshot after N writes (0 to disable)

### `Store` Methods

| Method | Description |
|---|---|
| `Set(key string, value []byte) error` | Set a key-value pair |
| `Get(key string) ([]byte, bool)` | Get a value by key |
| `Delete(key string) (bool, error)` | Delete a key, returns whether it existed |
| `Size() int` | Return number of entries |
| `Keys() []string` | Return all keys |
| `Snapshot() error` | Manually trigger a snapshot |
| `Close() error` | Sync and close the store |

### `NewHashTable[K comparable, V any](numShards int) *HashTable[K, V]`

Creates an in-memory hash table (no persistence). Use `shelf.Open()` for a persistent store.

## Architecture

### Storage Engine

```
data/
  wal.log        (append-only write-ahead log)
  snapshot.db    (compact point-in-time state dump)
```

**Write path**: Every `Set`/`Delete` appends a binary entry to `wal.log` (with CRC32 checksum), then applies to the in-memory hash table. WAL before memory guarantees durability.

**Recovery**: On `Open()`, shelf loads `snapshot.db` (if valid), then replays `wal.log` entries on top. Corrupt snapshots are detected via CRC and discarded. Partial WAL entries (from crashes) are truncated.

**Snapshots**: When writes exceed `snapshotThreshold`, shelf creates an atomic snapshot (temp file + rename), truncates the WAL, and resets the counter. This keeps the WAL bounded and recovery fast.

### Hash Table

- **Sharded** — N independent shards, each with its own `sync.RWMutex`
- **Separate chaining** — Collisions resolved with linked lists
- **Dynamic resizing** — Per-shard resize at 0.75 load factor
- **Hash function** — FNV-1a (64-bit)

### Concurrency Model

- **Reads** — `RLock` on the relevant shard only
- **Writes** — `Lock` on the relevant shard only
- **Global ops** (`Size`, `Keys`, `Snapshot`) — Lock all shards in order (deadlock-free)
- **WAL writes** — Serialized by `Store.mu`

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

## Configuration

### Server Flags

| Flag | Default | Description |
|---|---|---|
| `-addr` | `:8080` | Listen address |
| `-data` | `./shelf.db` | Data directory |
| `-shards` | `16` | Number of hash table shards |
| `-snapshot` | `10000` | Auto-snapshot threshold (0 to disable) |

### Tuning

- **More shards** → better read parallelism, more memory overhead
- **Lower snapshot threshold** → smaller WAL, faster recovery, more frequent snapshots
- **Higher snapshot threshold** → fewer snapshots, larger WAL, slower recovery

## Project Structure

```
shelf/
  hashmap.go           # Generic sharded hash table
  hashmap_test.go      # Hash table tests + benchmarks
  storage.go           # WAL persistence + snapshots
  storage_test.go      # Storage tests + benchmarks
  cmd/
    server/
      main.go          # HTTP REST server
      main_test.go     # Server tests + benchmarks
    demo/
      main.go          # Quick demo
```

## License

MIT
