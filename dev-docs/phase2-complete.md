# Phase 2 Complete: Clustering

## Implemented Components

### 1. DNS Discovery (discovery.go)
- ✅ Node discovery via DNS A records
- ✅ Continuous DNS monitoring with configurable retry interval
- ✅ Change detection (node additions/removals)
- ✅ Callback-based notification system
- ✅ IPv4 support

### 2. Cluster State (state.go)
- ✅ NATS JetStream KV integration
- ✅ Node information storage (NodeInfo struct)
  - ID, datacenter, IP, status
  - CPU/Memory resources (total/available)
  - Running processes list
  - Last seen timestamp with TTL (5 minutes)
- ✅ Service allocation tracking (ServiceAllocation struct)
  - Service-to-node mappings
  - Allocation status (pending/running/stopped/failed)
  - Version tracking, timestamps
- ✅ CRUD operations:
  - UpdateNode / GetNode / ListNodes / RemoveNode
  - CreateAllocation / GetAllocation / ListAllocations / UpdateAllocation / DeleteAllocation
- ✅ Watch mechanism for real-time node changes

### 3. Leader Election (leader.go)
- ✅ Leader election via NATS JetStream KV
- ✅ TTL-based lease (10 seconds with 5-second refresh)
- ✅ Automatic campaign for leadership
- ✅ Graceful step-down
- ✅ Leadership refresh to maintain lease
- ✅ IsLeader() status check
- ✅ GetLeader() to query current leader
- ✅ WaitForLeader() for startup synchronization

### 4. Integration

**Agent:**
- ✅ Cluster state initialization on startup
- ✅ Heartbeat publishing (every 5 seconds)
- ✅ Node info collection (processes, resources, status)
- ✅ Automatic node registration in cluster

**Server:**
- ✅ Leader election campaign on startup
- ✅ Leadership change detection
- ✅ Scheduler activation only on leader node
- ✅ DNS-based cluster node discovery
- ✅ Node change monitoring

## Architecture

```
Multi-node Cluster:

Node 1 (Leader):
  Server (scheduler active) + Agent
  ↓
NATS JetStream KV:
  - asty-leader bucket (leader election)
  - asty-cluster bucket (node info, allocations)
  ↑
Node 2-N (Followers):
  Server (scheduler inactive) + Agent

DNS A-records (A_DOMAIN):
  nodes.example.com → [Node1 IP, Node2 IP, Node3 IP]
```

## Communication Flow

1. **Node Registration:**
   - Agent starts → connects to NATS → initializes cluster state
   - Every 5s: Agent publishes NodeInfo to JetStream KV
   - TTL 5min: Stale nodes auto-expire

2. **Leader Election:**
   - Server starts → campaigns for leadership via KV
   - One server acquires "current-leader" key (10s TTL)
   - Leader refreshes lease every 5s
   - If leader crashes: TTL expires → new election

3. **Discovery:**
   - Server resolves A_DOMAIN every 15s
   - On DNS change: triggers re-evaluation callback

4. **Scheduling (leader only):**
   - Leader watches node heartbeats
   - Leader watches service allocations
   - Leader makes placement decisions
   - Leader sends commands to agents via NATS

## What Works

```bash
# Multiple agents can join cluster
asty -mode agent  # Node 1
asty -mode agent  # Node 2
asty -mode agent  # Node 3

# Multiple servers elect leader
asty -mode server # One becomes leader
asty -mode server # Others follow
```

- ✅ Agents register in cluster state
- ✅ Servers elect leader automatically
- ✅ DNS discovery tracks node changes
- ✅ State persists in NATS JetStream KV
- ✅ Leader failover works (TTL expiration)

## Known Limitations

1. **Resource detection**: Hardcoded CPU/Memory values (TODO: detect from system)
2. **IP detection**: Node IP not yet populated
3. **Scheduler**: Placeholder, no actual placement logic yet
4. **Commands**: Agent command handling incomplete
5. **Split-brain**: No fencing mechanism if NATS partitions

## Next: Phase 3 - Basic Scheduler

Ready to implement:
- System scheduler (one copy per node)
- Service scheduler (min 3 copies in different DCs)
- Simple placement (round-robin initially, before locality-aware)
- Agent command protocol (start/stop service)
