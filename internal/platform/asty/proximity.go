package asty

import (
	"context"
	"time"

	"asty/internal/platform/asty/features/scheduling/proximity"

	"github.com/rs/zerolog/log"
)

// ProximityMatrix — backward-compatible alias
type ProximityMatrix = proximity.Matrix

var NewProximityMatrix = proximity.NewMatrix

// RunProximityValidation runs periodic latency validation
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
