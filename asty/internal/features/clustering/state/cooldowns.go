package state

import (
	"encoding/json"
	"fmt"
	"time"

	"asty/asty/internal/core/types"

	"github.com/nats-io/nats.go"
)

const serviceCooldownKey = "service.%s.cooldown"

// GetServiceCooldown returns the cooldown record for a service. Missing
// keys come back as a zero-value ServiceCooldown rather than an error,
// so callers can treat "never scaled" and "scaled long ago" uniformly.
func (cs *ClusterState) GetServiceCooldown(service string) (types.ServiceCooldown, error) {
	key := fmt.Sprintf(serviceCooldownKey, service)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return types.ServiceCooldown{}, nil
		}
		return types.ServiceCooldown{}, fmt.Errorf("failed to get cooldown: %w", err)
	}
	var c types.ServiceCooldown
	if err := json.Unmarshal(entry.Value(), &c); err != nil {
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

func (cs *ClusterState) mutateCooldown(service string, fn func(*types.ServiceCooldown)) error {
	key := fmt.Sprintf(serviceCooldownKey, service)
	for attempt := 0; attempt < allocationMutateMaxRetries; attempt++ {
		entry, err := cs.bucket.Get(key)
		var (
			rev uint64
			c   types.ServiceCooldown
		)
		if err == nil {
			rev = entry.Revision()
			if err := json.Unmarshal(entry.Value(), &c); err != nil {
				return fmt.Errorf("unmarshal cooldown: %w", err)
			}
		} else if err != nats.ErrKeyNotFound {
			return fmt.Errorf("get cooldown: %w", err)
		}
		fn(&c)
		data, err := json.Marshal(&c)
		if err != nil {
			return fmt.Errorf("marshal cooldown: %w", err)
		}
		if rev == 0 {
			if _, err := cs.bucket.Create(key, data); err != nil {
				if isCASConflict(err) {
					continue
				}
				return fmt.Errorf("create cooldown: %w", err)
			}
		} else {
			if _, err := cs.bucket.Update(key, data, rev); err != nil {
				if isCASConflict(err) {
					continue
				}
				return fmt.Errorf("update cooldown: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("cooldown update conflict after %d retries", allocationMutateMaxRetries)
}
