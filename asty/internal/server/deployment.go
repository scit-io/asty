package server

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/deployer"
)

// Deployment timing defaults — used when a service .asty file leaves
// the corresponding update.* field blank. Picked to be safe for typical
// services: 10 s of healthy-time before promotion, 3 min to reach
// healthy, 10 min total before the deploy is declared stuck.
const (
	defaultMinHealthyTime   = 10 * time.Second
	defaultHealthyDeadline  = 3 * time.Minute
	defaultProgressDeadline = 10 * time.Minute
)

// DeployService initiates a service deployment. Pulls the canonical
// service definition from the loader (so .asty changes between
// allocations and now are honored), reads current allocations to set
// the rollout fan-out, and hands the plan to the deployer. KV buckets
// are provisioned in sendStartCommand — the single dispatch point for
// all start paths.
func (s *Server) DeployService(ctx context.Context, serviceName, version string) (*deployer.DeploymentStatus, error) {
	svc, err := s.serviceLoader.GetService(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to load service definition: %w", err)
	}

	allocs, err := s.clusterState.ListAllocations(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to list allocations: %w", err)
	}
	if len(allocs) == 0 {
		return nil, fmt.Errorf("no allocations found for service %s", serviceName)
	}

	currentVersion := allocs[0].Version
	if currentVersion == "" {
		currentVersion = "unknown"
	}

	healthyDeadline := types.ParseDurationOr(svc.Update.HealthyDeadline, defaultHealthyDeadline)
	minHealthy := types.ParseDurationOr(svc.Update.MinHealthyTime, defaultMinHealthyTime)
	progressDeadline := types.ParseDurationOr(svc.Update.ProgressDeadline, defaultProgressDeadline)

	plan := &deployer.DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: currentVersion,
		TargetVersion:  version,
		Allocations:    allocs,
		Service:        svc,
		UpdateStrategy: deployer.UpdateStrategy{
			MaxParallel:      svc.Update.GetMaxParallel(),
			MinHealthyTime:   minHealthy,
			HealthyDeadline:  healthyDeadline,
			ProgressDeadline: progressDeadline,
			AutoRevert:       svc.Update.AutoRevert,
			Canary:           svc.Update.Canary,
			CanaryRetries:    svc.Update.CanaryRetries,
		},
	}

	return s.deployer.Deploy(ctx, plan)
}
