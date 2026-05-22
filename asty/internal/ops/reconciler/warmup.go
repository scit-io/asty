package reconciler

import (
	"context"
	"time"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// warmupQuietWindow / warmupMaxWait gate the FIRST reconcile pass so
// initial placement uses the whole cluster rather than the subset of
// nodes that happened to heartbeat by the time leader election
// finished. The leader watches NodeReady transitions from the cluster
// KV; every new ready node resets the quiet timer. When the timer
// elapses (no new ready nodes for quietWindow) OR maxWait fires, the
// first enqueueAllServices runs. Event-driven — no polling sleep.
//
// Subsequent reconciles fire on watcher events and the safety-net
// resync as usual; warmup applies only once per leader incarnation.
const (
	warmupQuietWindow = 3 * time.Second
	warmupMaxWait     = 30 * time.Second
)

// runWarmup blocks until the cluster's NodeReady set stops changing
// for warmupQuietWindow or warmupMaxWait elapses. Pure event-driven:
// pipes node updates from the cluster-state KV watcher into a debounce
// loop. Every new NodeReady transition resets the quiet timer; when
// the timer fires without interruption, the cluster is considered
// stable enough for the initial placement pass.
//
// The reconcile workers are not started until this returns, so any
// node-change events the parallel watchNodesToQueue picks up during
// warmup accumulate in the queue and get processed in one batch after
// enqueueAllServices fires.
func (c *ServiceController) runWarmup(ctx context.Context) {
	quiet := time.NewTimer(warmupQuietWindow)
	defer quiet.Stop()
	maxWait := time.NewTimer(warmupMaxWait)
	defer maxWait.Stop()

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()

	nodeReady := make(chan struct{}, 16)
	seen := make(map[string]types.NodeStatus)
	go func() {
		_ = c.state.WatchNodes(watchCtx, func(n *types.NodeInfo) {
			prev, had := seen[n.ID]
			seen[n.ID] = n.Status
			if n.Status == types.NodeReady && (!had || prev != types.NodeReady) {
				select {
				case nodeReady <- struct{}{}:
				default:
				}
			}
		})
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-maxWait.C:
			log.Info().Int("ready_nodes", countReady(seen)).Msg("controller warmup: max wait elapsed, proceeding")
			return
		case <-quiet.C:
			log.Info().Int("ready_nodes", countReady(seen)).Msg("controller warmup: cluster settled, proceeding")
			return
		case <-nodeReady:
			if !quiet.Stop() {
				select {
				case <-quiet.C:
				default:
				}
			}
			quiet.Reset(warmupQuietWindow)
		}
	}
}

// countReady tallies NodeReady entries in the watcher's seen map for
// the warmup log line — operators want to know "how many nodes did we
// wait for" without grepping further.
func countReady(seen map[string]types.NodeStatus) int {
	n := 0
	for _, st := range seen {
		if st == types.NodeReady {
			n++
		}
	}
	return n
}
