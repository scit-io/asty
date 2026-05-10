package asty

import (
	"context"
	"time"

	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/observability/events"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// LoadConfig loads configuration from environment variables (A_* prefix).
func LoadConfig() (*Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// NewDeployer creates a Deployer with the legacy signature.
func NewDeployer(clusterState *ClusterState, nc *nats.Conn, cfg *Config) *Deployer {
	return deployment.NewDeployer(clusterState, nc, deployment.DeployerConfig{})
}

// EventBuffer is a fixed-size ring buffer of ClusterEvents. Thread-safe.
type EventBuffer struct {
	inner *events.Buffer
}

func NewEventBuffer(maxN int) *EventBuffer {
	return &EventBuffer{inner: events.NewBuffer(maxN)}
}

func (eb *EventBuffer) Add(e ClusterEvent) { eb.inner.Add(e) }

func (eb *EventBuffer) GetLast(n int) []ClusterEvent { return eb.inner.GetLast(n) }

// RunProximityValidation runs periodic latency validation.
func RunProximityValidation(ctx context.Context, pm *ProximityMatrix, clusterState *ClusterState) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodes, err := clusterState.ListNodes()
			if err != nil {
				log.Error().Err(err).Msg("failed to list nodes for latency validation")
				continue
			}
			pm.ValidateLatencies(ctx, nodes)
		}
	}
}

