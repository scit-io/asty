package asty

import (
	"testing"
	"time"
)

func newReadyNode(id, dc string) *NodeInfo {
	return &NodeInfo{
		ID:              id,
		Datacenter:      dc,
		Status:          "ready",
		LastSeen:        time.Now(),
		CPUAvailable:    1000,
		MemoryAvailable: 1024,
	}
}

// reconcileSystem should add a placement on every healthy node that lacks one.
func TestReconcileSystemAddsToAllNodes(t *testing.T) {
	cfg := &Config{MinCopies: 3, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*NodeInfo{
		newReadyNode("node1", "dc1"),
		newReadyNode("node2", "dc1"),
		newReadyNode("node3", "dc2"),
	}
	svc := &ServiceDefinition{
		Name:      "gateway",
		Type:      ServiceTypeSystem,
		Resources: Resources{CPU: 200, Memory: 64},
	}

	// No occupied nodes — picker should yield all three.
	picked := scheduler.pickCandidates(svc, nodes, map[string]bool{}, map[string]int{}, nil, len(nodes))
	if len(picked) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(picked))
	}
	seen := map[string]bool{}
	for _, n := range picked {
		seen[n.ID] = true
	}
	for _, n := range nodes {
		if !seen[n.ID] {
			t.Errorf("node %s missing from picks", n.ID)
		}
	}
}

// pickCandidates must produce a stable ordering when nodes are otherwise
// equal. This guards against the bug where sort.Slice on identical memory
// values shuffled placements between cycles and triggered rescheduling churn.
func TestPickCandidatesStableTiebreak(t *testing.T) {
	cfg := &Config{MinCopies: 2, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*NodeInfo{
		newReadyNode("nodeA", "dc1"),
		newReadyNode("nodeB", "dc1"),
		newReadyNode("nodeC", "dc1"),
	}
	svc := &ServiceDefinition{
		Name:      "xauth",
		Type:      ServiceTypeService,
		Resources: Resources{CPU: 100, Memory: 32},
	}

	first := scheduler.pickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nil, 2)
	for i := 0; i < 5; i++ {
		again := scheduler.pickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nil, 2)
		if len(again) != len(first) {
			t.Fatalf("candidate count changed between calls: %d vs %d", len(first), len(again))
		}
		for j := range first {
			if first[j].ID != again[j].ID {
				t.Fatalf("candidate order unstable at index %d: %s vs %s",
					j, first[j].ID, again[j].ID)
			}
		}
	}
}

// scheduleRegularService used to ignore datacenters when sortDatacentersByCapacity
// only iterated `minCopies` of them. The new picker biases toward DCs with
// fewer existing copies, so 3 picks across 3 DCs land in 3 distinct DCs.
func TestPickCandidatesGeoSpread(t *testing.T) {
	cfg := &Config{MinCopies: 3, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*NodeInfo{
		newReadyNode("a1", "dc1"),
		newReadyNode("a2", "dc1"),
		newReadyNode("b1", "dc2"),
		newReadyNode("c1", "dc3"),
	}
	svc := &ServiceDefinition{
		Name:      "xauth",
		Type:      ServiceTypeService,
		Resources: Resources{CPU: 100, Memory: 32},
	}

	picks := scheduler.pickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0, "dc2": 0, "dc3": 0}, nil, 3)
	if len(picks) != 3 {
		t.Fatalf("expected 3 picks, got %d", len(picks))
	}
	dcs := map[string]bool{}
	for _, n := range picks {
		dcs[n.Datacenter] = true
	}
	if len(dcs) < 3 {
		t.Errorf("expected picks across 3 DCs, got %v", dcs)
	}
}

// pickCandidates with packing pressure should prefer nodes that already
// host other services. Bootstrap of multiple services lands them on the same
// minimal set of nodes per DC instead of fanning out to every idle node.
func TestPickCandidatesPacking(t *testing.T) {
	cfg := &Config{MinCopies: 1, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*NodeInfo{
		newReadyNode("a1", "dc1"),
		newReadyNode("a2", "dc1"),
		newReadyNode("a3", "dc1"),
	}
	svc := &ServiceDefinition{
		Name:      "xhttp",
		Type:      ServiceTypeService,
		Resources: Resources{CPU: 100, Memory: 32},
	}

	// a2 already hosts 2 other services, a3 hosts 1, a1 hosts 0.
	// Picker should choose a2 first (most packed), then a3, then a1.
	nodeAllocCounts := map[string]int{"a1": 0, "a2": 2, "a3": 1}

	picks := scheduler.pickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nodeAllocCounts, 3)
	if len(picks) != 3 {
		t.Fatalf("expected 3 picks, got %d", len(picks))
	}
	want := []string{"a2", "a3", "a1"}
	for i, n := range picks {
		if n.ID != want[i] {
			t.Errorf("pick #%d: got %s, want %s", i, n.ID, want[i])
		}
	}
}

// targetCopies clamps MinCopies down to the number of healthy nodes so
// reconcileRegular doesn't loop forever trying to place the 3rd copy in a
// 2-node dev cluster.
func TestTargetCopiesClampedByNodeCount(t *testing.T) {
	scheduler := &Scheduler{cfg: &Config{MinCopies: 3}}
	if got := scheduler.targetCopies(2); got != 2 {
		t.Errorf("expected 2 (clamped), got %d", got)
	}
	if got := scheduler.targetCopies(5); got != 3 {
		t.Errorf("expected 3 (target), got %d", got)
	}
	if got := scheduler.targetCopies(0); got != 0 {
		t.Errorf("expected 0 with no nodes, got %d", got)
	}
}

// liveAllocations should treat pending/starting/running as live and exclude
// stopped/failed. Reconcile relies on this to count what's already in flight.
func TestLiveAllocations(t *testing.T) {
	allocs := []*ServiceAllocation{
		{NodeID: "n1", Status: "pending"},
		{NodeID: "n2", Status: "starting"},
		{NodeID: "n3", Status: "running"},
		{NodeID: "n4", Status: "stopped"},
		{NodeID: "n5", Status: "failed"},
	}
	got := liveAllocations(allocs)
	if len(got) != 3 {
		t.Fatalf("expected 3 live allocs, got %d", len(got))
	}
	wantIDs := map[string]bool{"n1": true, "n2": true, "n3": true}
	for _, a := range got {
		if !wantIDs[a.NodeID] {
			t.Errorf("unexpected live alloc on %s (%s)", a.NodeID, a.Status)
		}
	}
}

func TestSchedulerFilterHealthyNodes(t *testing.T) {
	scheduler := &Scheduler{cfg: &Config{}}

	nodes := []*NodeInfo{
		{ID: "n1", Status: "ready", LastSeen: time.Now()},
		{ID: "n2", Status: "draining", LastSeen: time.Now()},
		{ID: "n3", Status: "ready", LastSeen: time.Now().Add(-20 * time.Minute)},
		{ID: "n4", Status: "ready", LastSeen: time.Now()},
	}
	healthy := scheduler.filterHealthyNodes(nodes)
	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy nodes, got %d", len(healthy))
	}
	for _, n := range healthy {
		if n.Status != "ready" {
			t.Errorf("non-ready node leaked through: %s (%s)", n.ID, n.Status)
		}
		if time.Since(n.LastSeen) > nodeStaleAfter {
			t.Errorf("stale node leaked through: %s", n.ID)
		}
	}
}

func TestSchedulerHasResources(t *testing.T) {
	cfg := &Config{ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}
	node := &NodeInfo{CPUAvailable: 500, MemoryAvailable: 512}

	cases := []struct {
		name string
		req  Resources
		want bool
	}{
		{"sufficient", Resources{CPU: 200, Memory: 128}, true},
		{"cpu starved", Resources{CPU: 500, Memory: 128}, false},
		{"mem starved", Resources{CPU: 200, Memory: 512}, false},
		{"exact fit after reservation", Resources{CPU: 400, Memory: 262}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduler.hasResources(node, tc.req); got != tc.want {
				t.Errorf("hasResources(%+v)=%v want %v", tc.req, got, tc.want)
			}
		})
	}
}
