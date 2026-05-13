package server

import (
	"context"
	"fmt"
	"time"

	"asty/asty/internal/features/deployment"
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
func (s *Server) DeployService(ctx context.Context, serviceName, version string) (*deployment.DeploymentStatus, error) {
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

	healthyDeadline := parseUpdateDuration(svc.Update.HealthyDeadline, defaultHealthyDeadline)
	minHealthy := parseUpdateDuration(svc.Update.MinHealthyTime, defaultMinHealthyTime)
	progressDeadline := parseUpdateDuration(svc.Update.ProgressDeadline, defaultProgressDeadline)

	plan := &deployment.DeploymentPlan{
		ServiceName:    serviceName,
		CurrentVersion: currentVersion,
		TargetVersion:  version,
		Allocations:    allocs,
		Service:        svc,
		UpdateStrategy: deployment.UpdateStrategy{
			MaxParallel:      svc.Update.MaxParallel,
			MinHealthyTime:   minHealthy,
			HealthyDeadline:  healthyDeadline,
			ProgressDeadline: progressDeadline,
			AutoRevert:       svc.Update.AutoRevert,
			Canary:           1,
		},
	}

	return s.deployer.Deploy(ctx, plan)
}

// parseUpdateDuration parses a string from a .asty Update.* field with
// fallback for empty or invalid values. Centralised here so changing
// the parsing rule (e.g. supporting "1d") only touches one spot.
func parseUpdateDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
