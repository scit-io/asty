package autoscaling

import (
	"time"

	"github.com/rs/zerolog/log"
)

// inCooldown reports whether the most recent autoscaler action for
// `service` is still inside its configured cooldown window. We use the
// LARGER of CooldownUp/CooldownDown as the gate so a recent scale-up
// blocks a near-immediate scale-down (and vice-versa). This trades a
// little reactivity for a lot of stability under bursty load.
func (as *Autoscaler) inCooldown(service string) bool {
	last, ok := as.lastActionAt(service)
	if !ok {
		return false
	}
	cd := as.cfg.Autoscale.CooldownDown
	if as.cfg.Autoscale.CooldownUp > cd {
		cd = as.cfg.Autoscale.CooldownUp
	}
	return time.Since(last) < cd
}

// lastActionAt returns the timestamp of the most recent scale_up or
// scale_down recorded in KV. If neither has ever happened, ok is false.
func (as *Autoscaler) lastActionAt(service string) (time.Time, bool) {
	c, err := as.clusterState.GetServiceCooldown(service)
	if err != nil {
		log.Warn().Err(err).Str("service", service).Msg("failed to read cooldown; treating as not in cooldown")
		return time.Time{}, false
	}
	switch {
	case !c.LastScaleUp.IsZero() && !c.LastScaleDown.IsZero():
		if c.LastScaleUp.After(c.LastScaleDown) {
			return c.LastScaleUp, true
		}
		return c.LastScaleDown, true
	case !c.LastScaleUp.IsZero():
		return c.LastScaleUp, true
	case !c.LastScaleDown.IsZero():
		return c.LastScaleDown, true
	}
	return time.Time{}, false
}
