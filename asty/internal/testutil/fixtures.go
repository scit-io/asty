package testutil

import (
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
)

func NewTestConfig() *config.Config {
	return &config.Config{
		DevMode:    true,
		Domain:     "test.local",
		Datacenter: "dc1",
		NodeID:     "test-node-1",
		NodeIP:     "127.0.0.1",
		NATS: config.NATSConfig{
			Server: config.NATSServerConfig{Port: 4222},
		},
		Autoscale: config.AutoscaleConfig{
			MinCopies:           3,
			TargetCPU:           75,
			TargetMemory:        75,
			TrafficRPSThreshold: 5,
			TrafficWindow:       30 * time.Second,
			CooldownUp:          60 * time.Second,
			CooldownDown:        120 * time.Second,
			EvalInterval:        10 * time.Second,
			ControllerWorkers:   2,
		},
		Resources: config.ResourcesConfig{
			ReservedCPU:    100,
			ReservedMemory: 250,
		},
	}
}

func NewTestNode(id, dc string) *types.NodeInfo {
	return &types.NodeInfo{
		ID:              id,
		Datacenter:      dc,
		IP:              "10.0.0.1",
		Status:          types.NodeReady,
		CreatedAt:       time.Now(),
		LastSeen:        time.Now(),
		CPUTotal:        4000,
		CPUAvailable:    3000,
		MemoryTotal:     8192,
		MemoryAvailable: 6144,
	}
}

func NewTestService(name string, svcType types.ServiceType) *types.ServiceDefinition {
	return &types.ServiceDefinition{
		Name:    name,
		Type:    svcType,
		Command: "/bin/echo hello",
		Env:     map[string]string{},
		Resources: types.Resources{
			CPU:    200,
			Memory: 64,
		},
		Health: types.Health{
			Type:     "http",
			Path:     "/health",
			Interval: "10s",
			Timeout:  "3s",
		},
		Restart: types.Restart{
			Attempts: 3,
			Delay:    "5s",
		},
	}
}

func NewTestAllocation(service, nodeID string) *types.ServiceAllocation {
	return &types.ServiceAllocation{
		ID:           service + "-" + nodeID,
		ServiceName:  service,
		NodeID:       nodeID,
		Status:       types.AllocRunning,
		Version:      "v1.0.0",
		PID:          12345,
		StartedAt:    time.Now(),
		HealthStatus: types.HealthHealthy,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

func NewTestNodes(count int, dc string) []*types.NodeInfo {
	nodes := make([]*types.NodeInfo, count)
	for i := range nodes {
		id := dc + "-node-" + string(rune('1'+i))
		nodes[i] = NewTestNode(id, dc)
	}
	return nodes
}
