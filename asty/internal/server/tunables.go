package server

import "time"

// Server tunables. Promoted to constants so non-developers reading the
// boot sequence see "metricsRetention" instead of an unexplained
// 2 * time.Hour. Each constant documents *why* it has the magnitude it
// does.

const (
	// metricsRetention — how long the in-memory RPS time-series is kept.
	// 2 h covers any plausible autoscaling-window the user might
	// configure; older points are dropped to bound memory.
	metricsRetention = 2 * time.Hour

	// logBufferLines — how many recent log lines `<apiPrefix>/logs`
	// returns without subscribing. Big enough to debug a recent
	// incident, small enough to fit in memory across many sources.
	logBufferLines = 1000

	// eventBufferEntries — capacity of the cluster-events ring buffer
	// (alloc_failed, scale_up, node_join, …). Sized to keep ~hours of
	// history at typical event rates.
	eventBufferEntries = 10000

	// streamHubInterval — pure safety-net for missed KV-watch events.
	// The reactive path (debounced rebuilds on each watcher event) is
	// what drives normal updates; this fires only if no event has
	// arrived within the interval. 60 s is a sane "something must be
	// wrong if no events have happened in a minute" cadence.
	streamHubInterval = 60 * time.Second

	// resyncMultiplier — controller's safety-net resync runs at
	// EvalInterval × this factor; with EvalInterval=10 s the resync is
	// 60 s, far less aggressive than the per-event watchers.
	resyncMultiplier = 6

	// resyncCap — upper bound on resync interval for very large
	// EvalInterval values, so a misconfigured config doesn't push
	// safety-net resyncs into multi-hour territory.
	resyncCap = 5 * time.Minute

	// defaultResyncFallback applies when EvalInterval is unset.
	defaultResyncFallback = 60 * time.Second

	// devMockNodeCPU / Memory — synthetic resource sizes for fake
	// nodes the dev mode injects to demo scheduling without real
	// agents. Picked to look like a small cloud VM (4 vCPU @ 1 GHz,
	// 8 GiB RAM) with most of it free.
	devMockNodeCPUTotal    = 4000
	devMockNodeCPUAvail    = 3500
	devMockNodeMemoryTotal = 8192
	devMockNodeMemoryAvail = 6144
)
