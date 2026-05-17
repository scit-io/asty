package agent

import (
	"context"
	"os/exec"
	"slices"
	"sort"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

// natsPeerWatchInterval — how often watchNATSPeers re-resolves the peer
// list. 5s matches the agent's own heartbeat cadence: a node joining
// the cluster shows up in everyone else's peer list within one tick of
// its first heartbeat.
const natsPeerWatchInterval = 5 * time.Second

// natsRestartGrace is the SIGTERM-to-SIGKILL window during a planned
// nats-server restart. NATS shuts down JetStream cleanly in well under
// this on a quiet box; the cap exists so a wedged child doesn't pin
// the supervisor when peers change again.
const natsRestartGrace = 10 * time.Second

// superviseNATS owns the child nats-server lifecycle for the agent.
// Three things can happen at any time:
//   - ctx is cancelled → graceful stop, return.
//   - natsRestartCh fires (watchNATSPeers) → graceful stop, rebootstrap
//     with the freshest peer list, loop.
//   - the child exits on its own → fatal: running without the local
//     broker is meaningless, and we already issue restarts ourselves
//     on config changes.
func (a *Agent) superviseNATS(ctx context.Context) {
	for {
		cmd := a.natsServerCmd
		if cmd == nil || cmd.Process == nil {
			log.Fatal().Msg("nats-server supervisor: no child to watch")
			return
		}

		exitCh := make(chan error, 1)
		go func(c *exec.Cmd) { exitCh <- c.Wait() }(cmd)

		select {
		case <-ctx.Done():
			a.stopNATSChild(cmd, exitCh)
			return
		case <-a.natsRestartCh:
			log.Info().Msg("nats-server peer list changed, restarting child")
			a.stopNATSChild(cmd, exitCh)
			if err := a.bootstrapNATS(ctx); err != nil {
				log.Fatal().Err(err).Msg("nats-server restart: bootstrap failed")
				return
			}
		case err := <-exitCh:
			log.Fatal().Err(err).Msg("nats-server exited unexpectedly; agent cannot continue")
			return
		}
	}
}

// stopNATSChild sends SIGTERM and waits up to natsRestartGrace for the
// child to exit, escalating to SIGKILL on timeout. Drains exitCh in
// either case so the spawned Wait goroutine doesn't leak.
func (a *Agent) stopNATSChild(cmd *exec.Cmd, exitCh <-chan error) {
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-exitCh:
	case <-time.After(natsRestartGrace):
		log.Warn().Dur("grace", natsRestartGrace).Msg("nats-server didn't exit on SIGTERM, escalating to SIGKILL")
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-exitCh
	}
}

// watchNATSPeers polls the peer source (A_NATS_PEERS env in dev, DNS
// lookup of cfg.Domain in prod) and signals the supervisor to restart
// nats-server when the resolved set changes. The supervisor reads the
// fresh peer list via bootstrapNATS, so the watcher only carries the
// "something changed" signal — never the list itself.
func (a *Agent) watchNATSPeers(ctx context.Context) {
	nodeIP := a.resolveNodeIP()
	if nodeIP == "" {
		log.Warn().Msg("nats peer watcher disabled: cannot resolve local node IP")
		return
	}
	current := sortedPeers(a.resolveNATSPeers(nodeIP))

	ticker := time.NewTicker(natsPeerWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next := sortedPeers(a.resolveNATSPeers(nodeIP))
			if slices.Equal(current, next) {
				continue
			}
			log.Info().Strs("from", current).Strs("to", next).Msg("nats-server peers changed, requesting restart")
			current = next
			select {
			case a.natsRestartCh <- struct{}{}:
			default:
				// A restart is already queued; the supervisor will pick
				// up the freshest peer list when it services that
				// restart, so dropping this signal loses nothing.
			}
		}
	}
}

// sortedPeers returns a deep copy of in with entries sorted, so two
// peer lists differing only by order compare equal.
func sortedPeers(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
