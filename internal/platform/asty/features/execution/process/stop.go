package process

import (
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// Stop sends SIGTERM, waits up to the service's kill_timeout, then
// follows up with SIGKILL if the process is still alive. The signal is
// sent to the process group (negative PID) so any children die with
// the parent.
//
// A no-op if the process is not currently StatusRunning.
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != StatusRunning {
		return nil
	}
	p.status = StatusStopping

	if p.cancelCtx != nil {
		p.cancelCtx()
	}
	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("stopping process")

	syscall.Kill(-p.pid, syscall.SIGTERM)

	killTimeout := p.svc.GetKillTimeout()
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()

	select {
	case <-time.After(killTimeout):
		log.Warn().
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Dur("timeout", killTimeout).
			Msg("graceful shutdown timeout, sending SIGKILL")
		syscall.Kill(-p.pid, syscall.SIGKILL)
		<-done
	case err := <-done:
		if err != nil && err.Error() != "signal: terminated" {
			log.Warn().Err(err).Str("service", p.svc.Name).Msg("process exited with error")
		}
	}

	p.status = StatusStopped
	p.closeLogs()
	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("process stopped")
	return nil
}
