package kv

import (
	"errors"
	"fmt"
	"time"

	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go/jetstream"
)

const serviceCooldownKey = "service.%s.cooldown"

// GetServiceCooldown returns the cooldown record for a service. Missing
// keys come back as a zero-value ServiceCooldown rather than an error,
// so callers can treat "never scaled" and "scaled long ago" uniformly.
func (cs *ClusterState) GetServiceCooldown(service string) (types.ServiceCooldown, error) {
	key := fmt.Sprintf(serviceCooldownKey, service)
	ctx, cancel := kvCtx()
	defer cancel()
	entry, err := cs.bucket.Get(ctx, key)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return types.ServiceCooldown{}, nil
		}
		return types.ServiceCooldown{}, fmt.Errorf("failed to get cooldown: %w", err)
	}
	var c types.ServiceCooldown
	if err := codec.State.Unmarshal(entry.Value(), &c); err != nil {
		return types.ServiceCooldown{}, fmt.Errorf("failed to unmarshal cooldown: %w", err)
	}
	return c, nil
}

// MarkScaleUp persists "scale-up just happened".
func (cs *ClusterState) MarkScaleUp(service string, when time.Time) error {
	return cs.mutateCooldown(service, func(c *types.ServiceCooldown) { c.LastScaleUp = when })
}

// MarkScaleDown is the symmetric write for shrink events.
func (cs *ClusterState) MarkScaleDown(service string, when time.Time) error {
	return cs.mutateCooldown(service, func(c *types.ServiceCooldown) { c.LastScaleDown = when })
}

// MarkIdleSince persists the moment the service was first observed
// below the scale-down floor. The autoscaler calls it on every
// evaluation; passing a zero `when` clears the marker (service exited
// the idle window).
func (cs *ClusterState) MarkIdleSince(service string, when time.Time) error {
	return cs.mutateCooldown(service, func(c *types.ServiceCooldown) { c.IdleSince = when })
}

// SetRollbackFailed flips the per-service RollbackFailed flag. The
// deployer writes true after a failed rollback; the autoscaler reads
// it and bails out without acting; the operator clears it (false) via
// the API once they've reconciled the mixed-version state manually.
func (cs *ClusterState) SetRollbackFailed(service string, failed bool) error {
	return cs.mutateCooldown(service, func(c *types.ServiceCooldown) { c.RollbackFailed = failed })
}

// SetDeployInProgress toggles the per-service DeployInProgress gate
// the autoscaler watches. Deployer writes true on Deploy.Begin and
// false on any terminal state so a rollout and an autoscale event
// cannot interleave.
func (cs *ClusterState) SetDeployInProgress(service string, active bool) error {
	return cs.mutateCooldown(service, func(c *types.ServiceCooldown) { c.DeployInProgress = active })
}

func (cs *ClusterState) mutateCooldown(service string, fn func(*types.ServiceCooldown)) error {
	key := fmt.Sprintf(serviceCooldownKey, service)
	for attempt := 0; attempt < allocationMutateMaxRetries; attempt++ {
		ctx, cancel := kvCtx()
		entry, err := cs.bucket.Get(ctx, key)
		var (
			rev uint64
			c   types.ServiceCooldown
		)
		if err == nil {
			rev = entry.Revision()
			if err := codec.State.Unmarshal(entry.Value(), &c); err != nil {
				cancel()
				return fmt.Errorf("unmarshal cooldown: %w", err)
			}
		} else if !errors.Is(err, jetstream.ErrKeyNotFound) {
			cancel()
			return fmt.Errorf("get cooldown: %w", err)
		}
		fn(&c)
		data, err := codec.State.Marshal(&c)
		if err != nil {
			cancel()
			return fmt.Errorf("marshal cooldown: %w", err)
		}
		if rev == 0 {
			_, err = cs.bucket.Create(ctx, key, data)
		} else {
			_, err = cs.bucket.Update(ctx, key, data, rev)
		}
		cancel()
		if err != nil {
			if isCASConflict(err) {
				continue
			}
			return fmt.Errorf("write cooldown: %w", err)
		}
		return nil
	}
	return fmt.Errorf("cooldown update conflict after %d retries", allocationMutateMaxRetries)
}
