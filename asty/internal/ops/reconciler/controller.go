package reconciler

import (
	"context"
	"sync"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/ops/autoscaler"
	"asty/asty/internal/ops/scheduler"

	"github.com/rs/zerolog/log"
)

// defaultWorkers and defaultResync are fall-back values when the
// caller passes 0 (the typical "use defaults" Go pattern).
const (
	defaultWorkers = 2
	defaultResync  = 60 * time.Second
)

// failureLimitDefault — how many times a key can fail reconciliation
// before the workqueue's exponential backoff caps out at MaxDelay. The
// value is informative; the workqueue itself enforces the schedule.
const failureLimitDefault = 8

// SendStartCommand is the agent RPC the controller uses to ask an
// agent to start a service. Defined as a function type (not an
// interface with a single method) so callers can pass a method value
// directly without writing a wrapper struct.
type SendStartCommand func(nodeID string, svc *types.ServiceDefinition) error

// ServiceController is the leader-side reconciliation engine.
type ServiceController struct {
	state      *kv.ClusterState
	scheduler  *scheduler.Scheduler
	autoscaler *autoscaler.Autoscaler
	services   []*types.ServiceDefinition
	dispatch   SendStartCommand

	queue       *Workqueue
	workers     int
	resyncEvery time.Duration

	failureLimit int

	OnEvent func(types.ClusterEvent)
}

// NewServiceController wires the controller. workers<=0 and
// resyncEvery<=0 fall back to defaults.
func NewServiceController(
	st *kv.ClusterState,
	sched *scheduler.Scheduler,
	as *autoscaler.Autoscaler,
	services []*types.ServiceDefinition,
	dispatch SendStartCommand,
	workers int,
	resyncEvery time.Duration,
) *ServiceController {
	if workers <= 0 {
		workers = defaultWorkers
	}
	if resyncEvery <= 0 {
		resyncEvery = defaultResync
	}
	return &ServiceController{
		state:        st,
		scheduler:    sched,
		autoscaler:   as,
		services:     services,
		dispatch:     dispatch,
		queue:        NewWorkqueue(),
		workers:      workers,
		resyncEvery:  resyncEvery,
		failureLimit: failureLimitDefault,
	}
}

// Run starts the watchers, periodic resync and worker goroutines and
// blocks until ctx is cancelled. The first enqueueAllServices is
// deferred until the cluster has settled (see runWarmup) so initial
// placement spans the full intended node set rather than whichever
// subset happened to be Ready at leader-election time.
func (c *ServiceController) Run(ctx context.Context) {
	log.Info().Int("workers", c.workers).Dur("resync", c.resyncEvery).Msg("controller running")
	defer log.Info().Msg("controller stopped")

	go c.watchAllocsToQueue(ctx)
	go c.watchNodesToQueue(ctx)
	go c.periodicResync(ctx)

	c.runWarmup(ctx)
	if ctx.Err() != nil {
		return
	}
	c.enqueueAllServices()

	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c.runWorker(ctx, id)
		}(i)
	}

	<-ctx.Done()
	c.queue.ShutDown()
	wg.Wait()
}

// runWarmup is implemented in warmup.go alongside its constants —
// pulled out of controller.go to keep the file under the size cap.

func (c *ServiceController) runWorker(ctx context.Context, id int) {
	for {
		key, ok := c.queue.Get()
		if !ok {
			return
		}
		err := c.reconcile(ctx, key)
		if err != nil {
			log.Error().Err(err).Str("key", key).Int("worker", id).Msg("reconcile failed, requeuing with backoff")
			c.queue.AddRateLimited(key)
		} else {
			c.queue.Forget(key)
		}
		c.queue.Done(key)
	}
}

func (c *ServiceController) findService(name string) *types.ServiceDefinition {
	for _, s := range c.services {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func (c *ServiceController) enqueueAllServices() {
	for _, svc := range c.services {
		c.queue.Add(svc.Name)
	}
}

// Enqueue adds a service to the reconciliation workqueue. Used by API
// handlers (scale, restart, stop) to make changes visible without waiting
// for the periodic resync.
func (c *ServiceController) Enqueue(name string) {
	c.queue.Add(name)
}
