package agent

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"asty/asty/internal/core/natsconf"

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
//   - natsStopCh is closed (by Start, after graceful deregister) →
//     SIGTERM the child and return. Listening on this channel instead
//     of ctx.Done() preserves the ordering: deregister hits the local
//     KV via a still-live broker, *then* the broker dies.
//   - natsRestartCh fires (watchNATSPeers) → try a hot reload via
//     SIGHUP; fall back to a cold restart only when the JetStream
//     mode itself flips (standalone↔clustered). The cold-restart
//     bootstrap still uses ctx so a parent cancellation aborts it.
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

	wait:
		for {
			select {
			case <-a.natsStopCh:
				a.stopNATSChild(cmd, exitCh)
				return
			case <-a.natsRestartCh:
				if a.tryHotReloadNATS(cmd) {
					// Same process, same exitCh — keep waiting on it.
					continue wait
				}
				log.Info().Msg("nats-server peer list changed, cold restart")
				a.stopNATSChild(cmd, exitCh)
				if err := a.bootstrapNATS(ctx); err != nil {
					log.Fatal().Err(err).Msg("nats-server restart: bootstrap failed")
					return
				}
				break wait
			case err := <-exitCh:
				log.Fatal().Err(err).Msg("nats-server exited unexpectedly; agent cannot continue")
				return
			}
		}
	}
}

// tryHotReloadNATS writes the freshly-rendered nats.conf and signals
// the child with SIGHUP when the change is safe to apply live —
// meaning both the old and new conf carry a cluster{} block, so the
// JetStream mode (standalone vs clustered) does not flip. Returns
// false on any failure or mode flip; the caller then falls back to a
// cold restart.
func (a *Agent) tryHotReloadNATS(cmd *exec.Cmd) bool {
	nodeIP := a.resolveNodeIP()
	if nodeIP == "" {
		return false
	}
	// KeepClusterBlock=true: keep the process clustered through a
	// shrink to 1 node. The flip to standalone otherwise refuses to
	// load R>1 streams (10074). See natsconf.Render for the rule.
	newConf := natsconf.Render(natsconf.Input{
		Config:           a.cfg.NATS,
		NodeID:           a.nodeID,
		NodeIP:           nodeIP,
		Peers:            a.resolveNATSPeers(nodeIP),
		KeepClusterBlock: true,
	})

	oldRaw, err := os.ReadFile(a.natsConfPath())
	if err != nil {
		return false
	}
	if !natsConfCanHotReload(string(oldRaw), newConf) {
		return false
	}
	if string(oldRaw) == newConf {
		// Watcher fired but the rendered conf didn't actually change —
		// peer source produced equivalent output (e.g. duplicate entry
		// added then removed). Nothing to do.
		return true
	}
	if err := a.writeNATSConf(newConf); err != nil {
		log.Error().Err(err).Msg("nats-server hot-reload: write conf failed")
		return false
	}
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		log.Error().Err(err).Msg("nats-server hot-reload: SIGHUP failed")
		return false
	}
	log.Info().Msg("nats-server peer list changed, hot-reloaded via SIGHUP")
	return true
}

// natsConfCanHotReload reports whether NATS can apply the delta from
// oldConf to newConf without a process restart. The single guard is
// whether both conf strings still have a cluster{} block — flipping
// JetStream between standalone and clustered modes requires a fresh
// bootstrap, but a routes-list change inside an already-clustered
// node is exactly what SIGHUP exists for.
func natsConfCanHotReload(oldConf, newConf string) bool {
	return strings.Contains(oldConf, "cluster {") && strings.Contains(newConf, "cluster {")
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

// watchNATSPeers polls the peer source (DNS lookup of cfg.Domain) and
// signals the supervisor to restart
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
