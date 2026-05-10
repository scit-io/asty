package process

import (
	"context"

	"github.com/rs/zerolog/log"
)

// monitor is the goroutine spawned per Start: it blocks on Wait() and
// transitions the process state when the OS-level process exits. A clean
// exit during a Stop call lands as StatusStopped; an unexpected exit
// lands as StatusFailed and the agent's restart loop picks it up.
func (p *Process) monitor(ctx context.Context) {
	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	if p.status == StatusStopping {
		p.status = StatusStopped
	} else {
		p.status = StatusFailed
		log.Error().
			Err(err).
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Msg("process exited unexpectedly")
	}

	p.closeLogs()
}
