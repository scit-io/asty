package asty

import (
	"asty/internal/platform/asty/features/deployment"

	"github.com/nats-io/nats.go"
)

// Backward-compatible aliases
type Deployer = deployment.Deployer
type DeploymentRecord = deployment.DeploymentRecord
type DeploymentPlan = deployment.DeploymentPlan
type UpdateStrategy = deployment.UpdateStrategy
type DeploymentStatus = deployment.DeploymentStatus

func NewDeployer(clusterState *ClusterState, nc *nats.Conn, cfg *Config) *Deployer {
	return deployment.NewDeployer(clusterState, nc, deployment.DeployerConfig{})
}
