//go:build integration

package scheduler

import (
	"context"
	"testing"
	"time"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/kv"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
)

// startJetStream boots an embedded nats-server with JetStream enabled on a
// random port, backed by a temp store dir, and returns a connected client.
// This exercises the real (migrated) jetstream KV layer end-to-end — no
// mocks, no external cluster.
func startJetStream(t *testing.T) *nats.Conn {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	opts.JetStream = true
	opts.StoreDir = t.TempDir()
	srv := natsserver.RunServer(&opts)
	t.Cleanup(srv.Shutdown)

	nc, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// TestIntegration_GhostAllocReapedAndRescheduled reproduces "where did the
// services go" after a harsh N→1 and proves the fix end-to-end against a
// real JetStream KV:
//
//	A node is killed; its node.<id> leaves cluster KV but an allocation
//	record lingers with status "running". The scheduler must reap that
//	ghost and place a fresh copy on the surviving node — instead of
//	treating the dead-node copy as live and leaving the service down.
func TestIntegration_GhostAllocReapedAndRescheduled(t *testing.T) {
	nc := startJetStream(t)

	cs, err := kv.New(nc)
	if err != nil {
		t.Fatalf("kv.New: %v", err)
	}

	// One live node remains in cluster KV (the degradation survivor).
	if err := cs.UpdateNode(&types.NodeInfo{
		ID: "survivor", Status: types.NodeReady, LastSeen: time.Now(),
		CPUAvailable: 4000, MemoryAvailable: 4096,
	}); err != nil {
		t.Fatalf("UpdateNode: %v", err)
	}

	// A ghost: a "running" copy on a node that is gone from KV (killed,
	// its node.<id> removed, but the alloc record left behind).
	if err := cs.CreateAllocation(&types.ServiceAllocation{
		ServiceName: "xhttp", NodeID: "dead12", Status: types.AllocRunning,
	}); err != nil {
		t.Fatalf("CreateAllocation ghost: %v", err)
	}

	cfg := &config.Config{Autoscale: config.AutoscaleConfig{MinCopies: 1}}
	sched := NewScheduler(cs, cfg)
	svc := &types.ServiceDefinition{
		Name: "xhttp", Type: types.ServiceTypeService,
		Resources: types.Resources{CPU: 200, Memory: 64},
	}

	if err := sched.ReconcileService(context.Background(), svc); err != nil {
		t.Fatalf("ReconcileService: %v", err)
	}

	// The ghost on the dead node must be gone.
	if _, err := cs.GetAllocation("xhttp", "dead12"); err == nil {
		t.Error("ghost allocation on dead12 was not reaped")
	}

	// Exactly one copy must now exist, on the survivor.
	allocs, err := cs.ListAllocations("xhttp")
	if err != nil {
		t.Fatalf("ListAllocations: %v", err)
	}
	if len(allocs) != 1 {
		t.Fatalf("expected 1 allocation after reschedule, got %d: %+v", len(allocs), allocs)
	}
	if allocs[0].NodeID != "survivor" {
		t.Fatalf("expected the copy on survivor, got node %q", allocs[0].NodeID)
	}
}
