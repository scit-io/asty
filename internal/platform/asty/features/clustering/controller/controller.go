package controller

import (
	"context"
	"sync"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/scheduling"

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

// CommandDispatcher abstracts the agent RPC. The controller doesn't
// import server/ directly to avoid a cycle.
type CommandDispatcher interface {
	SendStartCommand(nodeID string, svc *types.ServiceDefinition) error
}

// ServiceController is the leader-side reconciliation engine.
type ServiceController struct {
	state      *state.ClusterState
	scheduler  *scheduling.Scheduler
	autoscaler *autoscaling.Autoscaler
	services   []*types.ServiceDefinition
	dispatcher CommandDispatcher

	queue       *Workqueue
	workers     int
	resyncEvery time.Duration

	failureLimit int

	OnEvent func(types.ClusterEvent)
}

// NewServiceController wires the controller. workers<=0 and
// resyncEvery<=0 fall back to defaults.
func NewServiceController(
	st *state.ClusterState,
	sched *scheduling.Scheduler,
	as *autoscaling.Autoscaler,
	services []*types.ServiceDefinition,
	dispatcher CommandDispatcher,
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
		dispatcher:   dispatcher,
		queue:        NewWorkqueue(),
		workers:      workers,
		resyncEvery:  resyncEvery,
		failureLimit: failureLimitDefault,
	}
}

// Run starts the watchers, periodic resync and worker goroutines and
// blocks until ctx is cancelled.
func (c *ServiceController) Run(ctx context.Context) {
	log.Info().Int("workers", c.workers).Dur("resync", c.resyncEvery).Msg("controller running")
	defer log.Info().Msg("controller stopped")

	c.enqueueAllServices()

	go c.watchAllocsToQueue(ctx)
	go c.watchNodesToQueue(ctx)
	go c.periodicResync(ctx)

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
