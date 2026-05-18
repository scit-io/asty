package deployer

import "asty/asty/internal/core/types"

// recordTouched appends an allocation that has been dispatched at the
// target version, so a subsequent rollback knows the exact set to
// walk back. Concurrent-safe because rolling dispatchBatch and canary
// dispatch may both run from the deployer's goroutine sequentially —
// the lock is cheap insurance against future parallelism.
func (d *Deployer) recordTouched(alloc *types.ServiceAllocation) {
	d.touchedMu.Lock()
	defer d.touchedMu.Unlock()
	for _, a := range d.touched {
		if a.ServiceName == alloc.ServiceName && a.NodeID == alloc.NodeID {
			return
		}
	}
	d.touched = append(d.touched, alloc)
}

// resetTouched clears the set at the start of every Deploy run.
func (d *Deployer) resetTouched() {
	d.touchedMu.Lock()
	defer d.touchedMu.Unlock()
	d.touched = d.touched[:0]
}

// touchedSnapshot returns a copy so the rollback loop iterates a
// stable view even if a concurrent dispatch were to append (it
// doesn't today, but the contract is cheaper than reasoning about it).
func (d *Deployer) touchedSnapshot() []*types.ServiceAllocation {
	d.touchedMu.Lock()
	defer d.touchedMu.Unlock()
	out := make([]*types.ServiceAllocation, len(d.touched))
	copy(out, d.touched)
	return out
}
