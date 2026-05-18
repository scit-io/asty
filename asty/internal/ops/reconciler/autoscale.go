package reconciler

import (
	"context"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// autoscaleOnce evaluates and (if non-noop) executes one autoscaling
// decision for svc. Called from reconcile only for non-system services
// — system services run one copy per node and don't autoscale.
func (c *ServiceController) autoscaleOnce(ctx context.Context, svc *types.ServiceDefinition) {
	d, err := c.autoscaler.EvaluateService(ctx, svc)
	if err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("autoscaler evaluate failed")
		return
	}
	if d.Action == types.ScaleNone {
		return
	}
	log.Info().
		Str("service", svc.Name).
		Str("action", string(d.Action)).
		Str("reason", d.Reason).
		Msg("autoscaling decision")

	if err := c.autoscaler.ExecuteScalingDecision(ctx, d, svc); err != nil {
		log.Error().Err(err).Str("service", svc.Name).Msg("autoscaler execute failed")
		return
	}
	if c.OnEvent == nil {
		return
	}
	target := d.TargetNode
	if d.Action == types.ScaleDown {
		target = d.RemoveNode
	}
	c.OnEvent(types.NewEvent(string(d.Action), svc.Name, target, d.Reason))
}
