package state

import (
	"fmt"

	"asty/asty/internal/core/codec"
)

const serviceScaleKey = "service.%s.scale"

// scaleOverride is the persisted manual scale set via the API. When present,
// it overrides AutoscaleConfig.MinCopies in both the scheduler's placement
// target and the autoscaler's scale-down floor.
type scaleOverride struct {
	Desired int `json:"desired"`
}

// GetServiceScale returns the operator-set desired copy count for a service.
// Second return is false when no override has been set.
func (cs *ClusterState) GetServiceScale(service string) (int, bool) {
	key := fmt.Sprintf(serviceScaleKey, service)
	entry, err := cs.bucket.Get(key)
	if err != nil {
		return 0, false
	}
	var o scaleOverride
	if err := codec.Unmarshal(entry.Value(), &o); err != nil {
		return 0, false
	}
	return o.Desired, true
}

// SetServiceScale persists the desired copy count for a service. count<0 is
// rejected; the autoscaler/scheduler enforce healthy-node cap on top.
func (cs *ClusterState) SetServiceScale(service string, count int) error {
	if count < 0 {
		return fmt.Errorf("scale count must be >= 0, got %d", count)
	}
	data, err := codec.Marshal(scaleOverride{Desired: count})
	if err != nil {
		return fmt.Errorf("marshal scale: %w", err)
	}
	key := fmt.Sprintf(serviceScaleKey, service)
	if _, err := cs.bucket.Put(key, data); err != nil {
		return fmt.Errorf("put scale: %w", err)
	}
	return nil
}
