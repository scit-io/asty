package scheduling

import (
	"testing"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
)

func newReadyNode(id, dc string) *types.NodeInfo {
	return &types.NodeInfo{
		ID:              id,
		Datacenter:      dc,
		Status:          "ready",
		LastSeen:        time.Now(),
		CPUAvailable:    1000,
		MemoryAvailable: 1024,
	}
}

func TestReconcileSystemAddsToAllNodes(t *testing.T) {
	cfg := &config.Config{MinCopies: 3, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*types.NodeInfo{
		newReadyNode("node1", "dc1"),
		newReadyNode("node2", "dc1"),
		newReadyNode("node3", "dc2"),
	}
	svc := &types.ServiceDefinition{
		Name:      "gateway",
		Type:      types.ServiceTypeSystem,
		Resources: types.Resources{CPU: 200, Memory: 64},
	}

	picked := scheduler.PickCandidates(svc, nodes, map[string]bool{}, map[string]int{}, nil, len(nodes))
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

func TestPickCandidatesStableTiebreak(t *testing.T) {
	cfg := &config.Config{MinCopies: 2, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*types.NodeInfo{
		newReadyNode("nodeA", "dc1"),
		newReadyNode("nodeB", "dc1"),
		newReadyNode("nodeC", "dc1"),
	}
	svc := &types.ServiceDefinition{
		Name:      "xauth",
		Type:      types.ServiceTypeService,
		Resources: types.Resources{CPU: 100, Memory: 32},
	}

	first := scheduler.PickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nil, 2)
	for i := 0; i < 5; i++ {
		again := scheduler.PickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nil, 2)
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

func TestPickCandidatesGeoSpread(t *testing.T) {
	cfg := &config.Config{MinCopies: 3, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*types.NodeInfo{
		newReadyNode("a1", "dc1"),
		newReadyNode("a2", "dc1"),
		newReadyNode("b1", "dc2"),
		newReadyNode("c1", "dc3"),
	}
	svc := &types.ServiceDefinition{
		Name:      "xauth",
		Type:      types.ServiceTypeService,
		Resources: types.Resources{CPU: 100, Memory: 32},
	}

	picks := scheduler.PickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0, "dc2": 0, "dc3": 0}, nil, 3)
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

func TestPickCandidatesPacking(t *testing.T) {
	cfg := &config.Config{MinCopies: 1, ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*types.NodeInfo{
		newReadyNode("a1", "dc1"),
		newReadyNode("a2", "dc1"),
		newReadyNode("a3", "dc1"),
	}
	svc := &types.ServiceDefinition{
		Name:      "xhttp",
		Type:      types.ServiceTypeService,
		Resources: types.Resources{CPU: 100, Memory: 32},
	}

	nodeAllocCounts := map[string]int{"a1": 0, "a2": 2, "a3": 1}

	picks := scheduler.PickCandidates(svc, nodes, map[string]bool{}, map[string]int{"dc1": 0}, nodeAllocCounts, 3)
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

func TestTargetCopiesClampedByNodeCount(t *testing.T) {
	scheduler := &Scheduler{cfg: &config.Config{MinCopies: 3}}
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

func TestLiveAllocations(t *testing.T) {
	allocs := []*types.ServiceAllocation{
		{NodeID: "n1", Status: "pending"},
		{NodeID: "n2", Status: "starting"},
		{NodeID: "n3", Status: "running"},
		{NodeID: "n4", Status: "stopped"},
		{NodeID: "n5", Status: "failed"},
	}
	got := LiveAllocations(allocs)
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
	scheduler := &Scheduler{cfg: &config.Config{}}

	nodes := []*types.NodeInfo{
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
	cfg := &config.Config{ReservedCPU: 100, ReservedMemory: 250}
	scheduler := &Scheduler{cfg: cfg}
	node := &types.NodeInfo{CPUAvailable: 500, MemoryAvailable: 512}

	cases := []struct {
		name string
		req  types.Resources
		want bool
	}{
		{"sufficient", types.Resources{CPU: 200, Memory: 128}, true},
		{"cpu starved", types.Resources{CPU: 500, Memory: 128}, false},
		{"mem starved", types.Resources{CPU: 200, Memory: 512}, false},
		{"exact fit after reservation", types.Resources{CPU: 400, Memory: 262}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scheduler.hasResources(node, tc.req); got != tc.want {
				t.Errorf("hasResources(%+v)=%v want %v", tc.req, got, tc.want)
			}
		})
	}
}
