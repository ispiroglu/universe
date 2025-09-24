### **Product Requirements Document: Project universeKV**

**Version:** 1.1
**Date:** September 24, 2025

### 1\. Introduction & Vision

**1.1. Problem Statement:**
Modern applications require data stores that are both scalable and resilient. Single-node databases cannot scale beyond the capacity of one machine and represent a single point of failure. Existing distributed databases are often complex and may provide stronger consistency guarantees than necessary for all use cases, sometimes at the cost of performance or availability.

**1.2. Vision:**
Project universeKV will be a distributed, sharded, and fault-tolerant Key-Value (KV) store designed for high scalability and availability. It will achieve this by implementing the Raft consensus algorithm for consistency *within* each shard and supporting an **eventually consistent** read model to maximize read throughput. The system will be built with clear, understandable components, prioritizing learning and exploration of advanced distributed systems concepts like dynamic cluster membership and automatic sharding.

-----

### 2\. System Architecture Overview

universeKV is composed of three primary components:

1.  **Routing Layer (Proxy or Smart Client):** The entry point for all client requests. It holds a map of the cluster's topology and uses a **Consistent Hashing** algorithm to route a key to the correct shard.
2.  **Cluster Metadata Store:** A small, dedicated Raft cluster that acts as the single source of truth for the cluster's topology—which shards exist, which nodes belong to each shard, and which node is the leader of each shard.
3.  **Data Shards (Raft Clusters):** The core of the system. The total dataset is partitioned across multiple shards. Each shard is an independent **Raft Cluster** of 3+ nodes, responsible for storing, replicating, and serving its slice of the data.

-----

### 3\. Project Folder Structure

A well-organized folder structure is crucial for a project of this complexity. Adhering to standard Go project layout conventions will make the code more maintainable, understandable, and easier to navigate.

```
universekv/
├── cmd/
│   ├── universekv/
│   │   └── main.go       # Main server application
│   └── universe-cli/
│       └── main.go       # Command-line client
├── configs/
│   └── cluster-example.yaml
├── internal/
│   ├── cluster/
│   │   └── topology.go   # Manages the shard map (the "source of truth")
│   ├── raft/
│   │   └── raft.go       # Core Raft logic for a single shard
│   ├── router/
│   │   └── router.go     # Uses the ring to route requests
│   ├── server/
│   │   └── grpc.go       # Public KV gRPC server
│   └── store/
│       ├── store.go      # Combines WAL and SwissMap
│       └── wal.go
├── pkg/
│   ├── client/
│   │   └── client.go
│   ├── proto/
│   │   ├── kv.proto
│   │   └── raft.proto
│   └── ring/
│       └── ring.go       # The generic consistent hashing algorithm
├── go.mod
└── Makefile
```

-----

### 4\. Functional Requirements (FR)

#### FR1: Cluster & Shard Management

  * **FR1.1 - Static Shard Definition:** The system **MUST** be able to start from a configuration file that defines the initial shard layout (e.g., Shard 1 consists of nodes A, B, C; Shard 2 of D, E, F).
  * **FR1.2 - Dynamic Shard Membership (Intra-Shard):** The system **MUST** support adding a new replica node to, and removing an existing replica node from, a running shard without downtime.
      * This will be achieved by implementing Raft's single-server change (or joint consensus) protocol.
      * The process will be initiated via an admin command.
      * Upon a membership change, the shard's Raft cluster will update its configuration, and this change **MUST** be registered in the central **Cluster Metadata Store**.
  * **FR1.3 - Shard Failure Logging (Phase 1):** If an entire shard loses its quorum (e.g., 2 out of 3 nodes fail) and becomes unavailable, the Routing Layer **MUST** detect this unavailability (e.g., through health checks) and log critical errors for all requests directed to that shard. The system will not yet attempt to heal itself.

#### FR2: Raft Consensus Implementation (Per Shard)

  * **FR2.1 - Leader Election:** Each shard **MUST** autonomously elect a single leader using the Raft election algorithm. If a leader fails, the remaining nodes in the shard **MUST** detect the failure via timeout and elect a new leader.
  * **FR2.2 - Log Replication:** The leader of a shard is solely responsible for accepting write requests (`SET`, `DELETE`). It **MUST** append these commands to its local Write-Ahead Log (WAL) and replicate them to a majority of its follower nodes before responding to the client.

#### FR3: Node Internal Architecture

  * **FR3.1 - State Machine (In-Memory Store):** Each node **MUST** maintain the actual KV data for its shard in a high-performance, in-memory hash map (**Swiss Table**). This state machine is the source for serving `GET` requests.
  * **FR3.2 - Concurrency Model:** The in-memory Swiss Table **MUST** be concurrency-safe, allowing multiple concurrent requests to be processed by a single node. This will be achieved using a `Mutex` or, for better performance, a `Read-Write Lock` that allows multiple concurrent reads.
  * **FR3.3 - Write-Ahead Log (WAL):** Each node **MUST** implement a WAL for durability.
      * **Detailed Flow:**
        1.  A leader receives a write command.
        2.  It serializes the command into a log entry and appends it to a file on disk (the WAL). The file is synced to ensure it's physically written.
        3.  The leader replicates this entry to its followers. Followers also write the entry to their own WAL files.
        4.  Once a majority of nodes have the entry on disk, the leader considers the entry "committed".
        5.  **Only after commitment** does the leader apply the command to its in-memory Swiss Table (the State Machine).
        6.  The leader then informs followers of the new commit index in subsequent heartbeats, and they apply the committed entries to their state machines.
  * **FR3.4 - Snapshotting & Compaction:** To manage disk usage and ensure fast restarts, each node **MUST** periodically create a snapshot of its in-memory Swiss Table and write it to disk. After a snapshot is successfully created, the node **MUST** be able to delete the WAL files that contain entries already covered by the snapshot.

#### FR4: Client API & Data Access

  * **FR4.1 - Write Operations:** The API **MUST** provide `SET <key> <value>` and `DELETE <key>` commands. These requests will be routed to the correct shard's leader.
  * **FR4.2 - Read Operations:** The API **MUST** provide a `GET <key>` command.
  * **FR4.3 - Follower Reads:** `GET` requests **CAN** be served by any node in a shard (Leader or Follower). This is a core design decision to increase read throughput and availability.

-----

### 5\. Non-Functional Requirements (NFR)

  * **NFR1 - Consistency Model: Eventual Consistency.** The system will provide strong consistency for writes via the Raft leader. However, because reads can be served by followers (**FR4.3**), read operations are **eventually consistent**. A client may read stale data from a follower that has not yet applied the latest committed log entry. This trade-off is explicitly accepted to achieve higher read scalability.
  * **NFR2 - Availability:** A single shard will remain available for both reads and writes as long as a majority of its nodes are operational. The overall system remains available as long as the Routing Layer, Metadata Store, and at least one shard are available.
  * **NFR3 - Performance:** The in-memory Swiss Table should ensure that `GET` requests are served with very low latency (target \< 1ms on an idle system).

-----

### 6\. Phased Rollout / Project Phases

#### Phase 1: Core Functionality

  * Implement the static, sharded cluster architecture.
  * Implement the full Raft protocol within each shard (Leader Election, Log Replication).
  * Implement the Node Internals (WAL, Swiss Table, Snapshotting).
  * Support follower reads with eventual consistency.
  * Implement basic shard failure logging (**FR1.3**).

#### Phase 2: Advanced Features (Future Work)

  * Implement dynamic shard membership (**FR1.2**).
  * Design and implement an "auto-sharding" or "rebalancing" mechanism. This system would monitor shards for size or load. If a shard becomes too large, a coordinator would orchestrate the splitting of the shard, creating a new shard, and moving a portion of the key-space. This would involve updating the Cluster Metadata Store to reflect the new topology. This is a highly complex feature.

-----

### 7\. Unresolved Questions & Discussion Points

#### Topic: Initial Data Population for a New Cluster

You asked how to handle the initial creation of shards from a large, existing dataset. A robust method is a multi-step, offline process:

1.  **Analysis Phase:** A command-line tool (`universe-analyzer`) is created. This tool reads the source dataset (e.g., from a massive CSV or another database dump) and analyzes the key distribution. Based on the desired number of shards, it generates a "shard plan" file. This plan maps specific key ranges to each target shard.
2.  **Loading Phase:** A second tool (`universe-loader`) reads the shard plan. It then connects to the newly started (but empty) universeKV cluster. For each key in the source data, it uses the plan to determine the correct shard and sends a `SET` command to that shard's leader. This process can be parallelized to load data into all shards simultaneously.

This offline approach provides a clean, deterministic way to bootstrap a large cluster.

-----

### 8\. Out of Scope for All Phases

  * Transactions spanning multiple keys or shards.
  * Advanced security features (TLS, authentication, authorization).
  * Client libraries for specific languages. The initial API will be a simple, raw protocol.