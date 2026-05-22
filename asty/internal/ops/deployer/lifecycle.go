package deployer

import (
	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// claim marks svc as currently deploying. Returns false if another
// run is already in flight on the same Deployer instance — caller
// surfaces that to the operator as a 409 instead of starting a
// second concurrent deploy that would clobber the first.
func (d *Deployer) claim(svc string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inFlight[svc] {
		return false
	}
	d.inFlight[svc] = true
	return true
}

// release flips the in-flight bit back off. Always paired with a
// successful claim via defer in the caller.
func (d *Deployer) release(svc string) {
	d.mu.Lock()
	delete(d.inFlight, svc)
	d.mu.Unlock()
}

// IsInFlight reports whether svc currently has a deploy running.
// Used by the dashboard to fail-fast on double-clicks before the
// deploy gate (set after the first record write) is visible.
func (d *Deployer) IsInFlight(svc string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inFlight[svc]
}

// pinVersion writes the service-level version pair. Errors are logged
// rather than returned because deploy success/failure has already been
// decided by the surrounding logic — losing the pin is a regression
// the next reconcile will surface, not a reason to fail the deploy.
func (d *Deployer) pinVersion(service string, v types.ServiceVersion) {
	if err := d.clusterState.SetServiceVersion(service, v); err != nil {
		log.Warn().Err(err).Str("service", service).Msg("deployment: failed to persist version pin")
	}
}

// setDeployGate toggles the autoscaler-skip flag. Best-effort: if the
// write fails the autoscaler may briefly race the deploy, but the
// deploy itself is unaffected so we don't surface this as an error.
func (d *Deployer) setDeployGate(service string, active bool) {
	if err := d.clusterState.SetDeployInProgress(service, active); err != nil {
		log.Warn().Err(err).Str("service", service).Bool("active", active).Msg("deployment: failed to toggle autoscaler gate")
	}
}
