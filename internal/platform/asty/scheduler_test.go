package asty

import (
	"testing"
	"time"
)

func TestSchedulerSystemService(t *testing.T) {
	// Create mock nodes
	nodes := []*NodeInfo{
		{
			ID:              "node1",
			Datacenter:      "dc1",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
		{
			ID:              "node2",
			Datacenter:      "dc1",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
		{
			ID:              "node3",
			Datacenter:      "dc2",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
	}

	cfg := &Config{
		MinCopies:      3,
		ReservedCPU:    100,
		ReservedMemory: 250,
	}

	scheduler := &Scheduler{cfg: cfg}

	// Test system service - should place on all nodes
	svc := &ServiceDefinition{
		Name: "gateway",
		Type: ServiceTypeSystem,
		Resources: Resources{
			CPU:    200,
			Memory: 64,
		},
	}

	placements, err := scheduler.scheduleSystemService(svc, nodes)
	if err != nil {
		t.Fatalf("failed to schedule system service: %v", err)
	}

	if len(placements) != 3 {
		t.Errorf("expected 3 placements, got %d", len(placements))
	}

	// Verify all nodes are used
	nodeMap := make(map[string]bool)
	for _, p := range placements {
		nodeMap[p.NodeID] = true
	}

	for _, node := range nodes {
		if !nodeMap[node.ID] {
			t.Errorf("node %s not used in placements", node.ID)
		}
	}
}

func TestSchedulerRegularService(t *testing.T) {
	nodes := []*NodeInfo{
		{
			ID:              "node1",
			Datacenter:      "dc1",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
		{
			ID:              "node2",
			Datacenter:      "dc1",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
		{
			ID:              "node3",
			Datacenter:      "dc2",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
		{
			ID:              "node4",
			Datacenter:      "dc3",
			Status:          "ready",
			LastSeen:        time.Now(),
			CPUAvailable:    1000,
			MemoryAvailable: 1024,
		},
	}

	cfg := &Config{
		MinCopies:      3,
		ReservedCPU:    100,
		ReservedMemory: 250,
	}

	scheduler := &Scheduler{cfg: cfg}

	svc := &ServiceDefinition{
		Name: "xauth",
		Type: ServiceTypeService,
		Resources: Resources{
			CPU:    100,
			Memory: 32,
		},
	}

	placements, err := scheduler.scheduleRegularService(svc, nodes)
	if err != nil {
		t.Fatalf("failed to schedule service: %v", err)
	}

	if len(placements) != 3 {
		t.Errorf("expected 3 placements, got %d", len(placements))
	}

	// Verify geo-diversity: different datacenters
	dcMap := make(map[string]bool)
	for _, p := range placements {
		for _, node := range nodes {
			if node.ID == p.NodeID {
				dcMap[node.Datacenter] = true
				break
			}
		}
	}

	if len(dcMap) < 2 {
		t.Errorf("expected placements in at least 2 datacenters, got %d", len(dcMap))
	}
}

func TestSchedulerFilterHealthyNodes(t *testing.T) {
	cfg := &Config{}
	scheduler := &Scheduler{cfg: cfg}

	nodes := []*NodeInfo{
		{
			ID:       "node1",
			Status:   "ready",
			LastSeen: time.Now(),
		},
		{
			ID:       "node2",
			Status:   "draining",
			LastSeen: time.Now(),
		},
		{
			ID:       "node3",
			Status:   "ready",
			LastSeen: time.Now().Add(-20 * time.Minute), // Stale
		},
		{
			ID:       "node4",
			Status:   "ready",
			LastSeen: time.Now(),
		},
	}

	healthy := scheduler.filterHealthyNodes(nodes)

	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy nodes, got %d", len(healthy))
	}

	for _, node := range healthy {
		if node.Status != "ready" {
			t.Errorf("unhealthy node in result: %s (status=%s)", node.ID, node.Status)
		}
		if time.Since(node.LastSeen) > 10*time.Minute {
			t.Errorf("stale node in result: %s", node.ID)
		}
	}
}

func TestSchedulerResourceCheck(t *testing.T) {
	cfg := &Config{
		ReservedCPU:    100,
		ReservedMemory: 250,
	}
	scheduler := &Scheduler{cfg: cfg}

	node := &NodeInfo{
		CPUAvailable:    500,
		MemoryAvailable: 512,
	}

	tests := []struct {
		name     string
		required Resources
		want     bool
	}{
		{
			name:     "sufficient resources",
			required: Resources{CPU: 200, Memory: 128},
			want:     true,
		},
		{
			name:     "insufficient CPU",
			required: Resources{CPU: 500, Memory: 128},
			want:     false,
		},
		{
			name:     "insufficient memory",
			required: Resources{CPU: 200, Memory: 512},
			want:     false,
		},
		{
			name:     "exact fit after reservation",
			required: Resources{CPU: 400, Memory: 262},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scheduler.hasResources(node, tt.required)
			if got != tt.want {
				t.Errorf("hasResources() = %v, want %v (CPU: %d/%d, Mem: %d/%d)",
					got, tt.want,
					tt.required.CPU, node.CPUAvailable-cfg.ReservedCPU,
					tt.required.Memory, node.MemoryAvailable-int64(cfg.ReservedMemory))
			}
		})
	}
}
