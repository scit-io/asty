package deployer

import (
	"testing"
	"time"

	"asty/asty/internal/core/types"
)

func TestDeploymentPlan(t *testing.T) {
	plan := &DeploymentPlan{
		ServiceName:    "test-service",
		CurrentVersion: "v1.0.0",
		TargetVersion:  "v1.1.0",
		Allocations: []*types.ServiceAllocation{
			{ServiceName: "test-service", NodeID: "node1", Status: types.AllocRunning, Version: "v1.0.0"},
			{ServiceName: "test-service", NodeID: "node2", Status: types.AllocRunning, Version: "v1.0.0"},
			{ServiceName: "test-service", NodeID: "node3", Status: types.AllocRunning, Version: "v1.0.0"},
		},
		UpdateStrategy: UpdateStrategy{
			MaxParallel:     1,
			MinHealthyTime:  10 * time.Second,
			HealthyDeadline: 3 * time.Minute,
			AutoRevert:      true,
			Canary:          1,
		},
	}

	if plan.ServiceName != "test-service" {
		t.Errorf("expected service name 'test-service', got '%s'", plan.ServiceName)
	}
	if len(plan.Allocations) != 3 {
		t.Errorf("expected 3 allocations, got %d", len(plan.Allocations))
	}
	if plan.UpdateStrategy.Canary != 1 {
		t.Errorf("expected canary=1, got %d", plan.UpdateStrategy.Canary)
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		got := min(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestDeploymentStatus(t *testing.T) {
	status := &DeploymentStatus{
		ServiceName:   "test-service",
		Status:        StateRunning,
		Phase:         PhaseCanary,
		Updated:       1,
		Total:         3,
		CanaryHealthy: true,
		StartTime:     time.Now(),
	}

	if status.Phase != PhaseCanary {
		t.Errorf("expected phase %q, got %q", PhaseCanary, status.Phase)
	}

	if !status.CanaryHealthy {
		t.Error("expected canary to be healthy")
	}

	if status.Updated > status.Total {
		t.Errorf("updated (%d) should not exceed total (%d)", status.Updated, status.Total)
	}
}

func TestUpdateStrategy(t *testing.T) {
	strategy := UpdateStrategy{
		MaxParallel:      2,
		MinHealthyTime:   15 * time.Second,
		HealthyDeadline:  5 * time.Minute,
		ProgressDeadline: 15 * time.Minute,
		AutoRevert:       true,
		Canary:           1,
	}

	if strategy.MaxParallel != 2 {
		t.Errorf("expected max_parallel=2, got %d", strategy.MaxParallel)
	}

	if strategy.MinHealthyTime != 15*time.Second {
		t.Errorf("expected min_healthy_time=15s, got %v", strategy.MinHealthyTime)
	}

	if !strategy.AutoRevert {
		t.Error("expected auto_revert=true")
	}
}
