# Store Layer

The store layer is responsible for delivering a durable key/value dictionary for the distributed database. It combines an in-memory hash map (for fast lookups) with a write-ahead log (for crash safety) so that every mutation can be replayed after a restart.

## Architecture at a Glance

- **In-memory index** – `github.com/mhmtszr/concurrent-swiss-map` provides the `CsMap[string, []byte]` that backs reads with low latency and thread-safe access.
- **Write-ahead log (WAL)** – an append-only file where every `SET` and `DELETE` operation is recorded before the in-memory state is updated.
- **Recovery** – on startup the store replays the WAL, rebuilding the index even if the process terminated uncleanly.

```
       ┌──────────────┐        append (length + JSON)
SET → │   Store.Set   │ ─────────────────────────────┐
       └──────┬───────┘                               │
              │                                       ▼
       ┌──────▼──────┐                       ┌────────────────┐
       │   WAL.Append│                       │ walPath.log    │
       └──────┬──────┘                       └────────┬───────┘
              │  update in-memory                     │
              ▼                                       │ replay
       ┌──────────────┐ ←─────────────── GET ─────────┘
       │ concurrent map│
       └──────────────┘
```

## Components

### In-Memory Map

- Backed by `CsMap[string, []byte]`.
- Outbound reads are copy-on-read: `Store.Get` returns a fresh byte slice so callers cannot mutate internal state.
- Writes (`Store.Set`) store a cloned copy of the input value before exposing it to the map.

### Write-Ahead Log (WAL)

- Stored at the path supplied to `store.New(path)`.
- Created with `os.OpenFile(path, O_CREATE|O_RDWR|O_APPEND)`; parent directories are created on demand.
- Serialized entries use JSON and are length-prefixed with a 4-byte big-endian unsigned integer.
- `Wal.Append` flushes and `fsync`s on every call to guarantee durability once the method returns.
- Concurrency is protected with an internal mutex; appends and reads cannot race.

### Recovery Loop

- `Store.Recover` calls `WAL.ReadAll` at construction time.
- The WAL reader flushes buffered bytes, seeks to the beginning, then iterates until EOF.
- Each entry is applied in order via `Store.applyEntry`.
- Unknown entry types are ignored to keep recovery tolerant to forward-compatible changes.

## Operations

### `Set`

1. Validate the key is non-empty.
2. Clone the value to prevent external mutation.
3. Append `{type:"set", key, value}` to the WAL and flush/fsync.
4. Update the in-memory map.

### `Delete`

1. Validate the key is non-empty.
2. Append `{type:"delete", key}` to the WAL.
3. Remove the key from the map, returning whether a value previously existed.

### `Get`

- Reads directly from the concurrent map without touching the WAL.
- Returns a copied value slice and a boolean flag indicating presence.

### `Close`

- Flushes remaining bytes, calls `fsync`, then closes the underlying file handle.

## WAL Record Format

Each record is stored as:

```
+------------+-------------------------+
| length (4) |  JSON payload (length)  |
+------------+-------------------------+
```

- Length is a 4-byte big-endian unsigned integer.
- JSON payload matches the `WALEntry` struct:

```json
{
  "type": "set" | "delete",
  "key": "…",
  "value": "base64-encoded binary" // omitted for delete
}
```

- Invalid length or truncated payload triggers `ErrCorruptWAL` during `ReadAll`.

## Concurrency & Durability

- `Store.Set` and `Store.Delete` share a mutex that serializes WAL appends and map updates.
- `CsMap` handles multi-reader access, so `Store.Get` does not need extra synchronization.
- Every append is followed by `Flush()` and `Sync()`: once these calls return, data is on disk (subject to underlying filesystem guarantees).

## API Overview

| Method              | Description                                                   |
|---------------------|---------------------------------------------------------------|
| `store.New(path)`   | Opens/creates WAL, replays recovery, returns ready-to-use store. |
| `(*Store).Set`      | Stores a value and logs the mutation.                          |
| `(*Store).Get`      | Retrieves a copy of the value.                                |
| `(*Store).Delete`   | Removes a key and logs the mutation, returns `true` if present. |
| `(*Store).Recover`  | Replays the WAL manually (already run in `New`).               |
| `(*Store).Close`    | Flushes and closes the WAL file.                               |

## Usage Example

```go
package main

import (
    "fmt"
    "log"

    "universe/internal/store"
)

func main() {
    kv, err := store.New("/var/lib/universe/store.wal")
    if err != nil {
        log.Fatalf("start store: %v", err)
    }
    defer kv.Close()

    if err := kv.Set("user:123", []byte(`{"name":"Ada"}`)); err != nil {
        log.Fatalf("set: %v", err)
    }

    if value, ok := kv.Get("user:123"); ok {
        fmt.Println(string(value))
    }

    if deleted, err := kv.Delete("user:123"); err != nil {
        log.Fatalf("delete: %v", err)
    } else if deleted {
        fmt.Println("removed user:123")
    }
}
```

## Operational Considerations

- **File growth** – the WAL is append-only; plan for compaction (snapshot + WAL truncate) as the dataset grows.
- **Corruption handling** – `ReadAll` surfaces `ErrCorruptWAL` when it encounters inconsistent length prefixes or truncated payloads. In production, consider checkpointing and alerting.
- **Permissions** – ensure the process can create the WAL directory (`0755`) and file (`0644`).
- **Backups** – durable state is `walPath.log`. Backups can copy the file while the process is running (appends are atomic per record).

## Future Enhancements

- **Snapshots** – periodic snapshots would shorten recovery time and cap WAL growth.
- **Segmented WAL** – rotating files simplifies retention and minimizes risk of single-file corruption.
- **Batching** – grouping multiple writes before `fsync` trades durability latency for throughput.
- **Checksums** – add a checksum to each entry to detect silent data corruption.

## How to Resolve "fsync Every Request" Technical Debt

Currently the implementation performs an `fsync` (via `file.Sync`) after **every** mutation. This guarantees the smallest possible data loss window (only the in-flight operation) but severely limits throughput because each write pays a full flush latency. Mature systems employ batching, group commit, configurable durability levels, or replication to amortize the cost.

### Strategy Matrix

| Strategy | Description | Crash Loss Window | Latency | Throughput | Complexity | Used By |
|----------|-------------|-------------------|---------|------------|------------|---------|
| Per-write sync | `fsync` each append | ~0 (last op only) | Highest | Lowest | Low | Safety mode, tests |
| Batch by count (Every N) | Sync after N appends | Up to N ops | Lower | Higher | Low | Cassandra (batch), RocksDB (optional) |
| Time-based interval | Sync every T ms | Up to T ms | Predictable | High | Medium | MongoDB journal (≈100ms), Redis AOF everysec, Cassandra periodic |
| Hybrid (count OR time) | Sync when either threshold triggers | Min(N window, T window) | Balanced | High | Medium | PostgreSQL group commit style |
| Manual / Async | Never auto-sync; explicit `Sync()` | Unbounded until sync | Lowest | Highest | Low | Benchmarks, ephemeral caches |
| Group commit goroutine | Channel + single flusher merges writers | Up to batch window | Low (per op) | Very High | Medium | PostgreSQL, many Raft impls |
| Majority replication + relaxed local sync | Rely on quorum for logical durability | Possibly recent local ops if multi-node loss | Medium | High | High | etcd (Raft), Mongo majority writes |

### Market Implementations (Simplified)

- **etcd (Raft)**: Appends to WAL and (by default) syncs each entry or micro-batch; durability also requires majority replication. Offers unsafe flag to skip fsync for benchmarks. Uses snapshots to truncate log.
- **MongoDB (WiredTiger)**: Journals with group commit (~100ms). Write concern lets clients require journaling (`j:true`) and/or majority replication. Checkpoints every 60s flush data files.
- **Cassandra**: Commit log `periodic` mode fsyncs at a configurable interval (default 100ms). Optional `batch` mode forces per-write sync. Data written first to commit log + memtable; memtables flush later to SSTables.
- **Redis**: AOF persistence modes: `always`, `everysec` (time-based), or `no` (OS flush). Allows operators to pick durability vs performance.
- **PostgreSQL**: Group commit: multiple transactions flush WAL together; synchronous commit settings let clients opt into waiting for flush or replication.
- **RocksDB/Pebble**: WriteOptions decide `sync=true|false`. Turning off sync yields large throughput gains; periodic flush or checkpoint provides durability boundary.
- **Couchbase**: Acknowledges in-memory then persists/replicates asynchronously; durability options (majority, majorityAndPersistActive) let clients escalate.

### Trade-offs

- **Lower latency vs durability**: Removing per-write fsync reduces tail latency drastically but expands the potential loss window.
- **Replication interplay**: If you add Raft/consensus, majority commit can allow relaxing local fsync frequency without risking acknowledged writes (unless a quorum fails before flush).
- **Operational tuning**: Time-based intervals simplify reasoning ("at most 10ms of data"), while count-based controls align with workload intensity.

### Recommended Evolution Path

1. **Introduce Sync Policy Abstraction**: `SyncAlways`, `SyncEveryN(int)`, `SyncInterval(time.Duration)`, `SyncManual`.
2. **Implement Hybrid**: Run a background flusher that triggers on either interval or count threshold.
3. **Expose Store Options**: Functional options (e.g. `store.WithSyncEveryN(128)`). Default to safer `SyncEveryN(1)` (current) or a moderate interval like 10ms.
4. **Add Manual `Sync()`**: Allow upper layers (e.g. snapshot or graceful shutdown) to force a durability boundary.
5. **Metrics & Observability**: Track average batch size, sync duration, pending entries—feed into adaptive tuning.
6. **Replication Integration** (future): After Raft majority commit, classify entries as "logically durable" even if not yet fsynced, letting you safely lengthen intervals.

### Suggested Initial Implementation (Minimal Code Delta)

```go
type SyncPolicy int
const (
    SyncAlways SyncPolicy = iota
    SyncEveryN
    SyncManual
)

type WALConfig struct {
    Path    string
    Policy  SyncPolicy
    EveryN  int // used with SyncEveryN
}

// Pseudocode inside Append:
// write bytes -> writer.Flush()
// if policy == SyncAlways -> file.Sync()
// else if policy == SyncEveryN && (count%EveryN==0) -> file.Sync()
```

### Crash Recovery Implications

- Un-synced, flushed entries will likely survive a clean shutdown but may be lost on power failure.
- A partially written record (crash mid-append) is detected by length/payload mismatch; remaining tail is discarded during recovery.

### When to Still Use Per-Write fsync

- Early development correctness validation.
- Low write volume / high value data (configuration, consensus metadata) where latency is acceptable.
- Before implementing WAL compaction & replication (simpler failure model).

### Future Extensions

- **Checksums per record**: Distinguish torn writes from logic errors.
- **Segment rotation**: Sync segment header/trailer instead of each record.
- **Adaptive policy**: Shorten intervals under low load; lengthen under bursts.

---

This section documents the technical debt and planned mitigation path so future contributors understand why per-write fsync exists now and how to evolve beyond it.
