package server

import (
	"sync"
	"testing"

	"asty/asty/internal/core/types"
)

// TestAllocIndex_RaceWritersVsSnapshot stresses the index from many
// writers (mirrors KV watch goroutines) while a reader keeps snapshotting.
// Failure mode under `-race`: a missing mutex around the maps would flag
// "DATA RACE" between map iteration in snapshot() and map writes in
// onNode/onAlloc.
func TestAllocIndex_RaceWritersVsSnapshot(t *testing.T) {
	idx := newAllocIndex()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Two writer goroutines per resource — exercise both upsert and
	// delete paths.
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func(prefix int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				idx.onNode(&types.NodeInfo{ID: "n", Status: types.NodeReady})
				idx.onNode(&types.NodeInfo{ID: "n", Status: types.NodeDeleted})
			}
		}(i)
		go func(prefix int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				idx.onAlloc(&types.ServiceAllocation{ServiceName: "s", NodeID: "n", Status: types.AllocRunning})
				idx.onAlloc(&types.ServiceAllocation{ServiceName: "s", NodeID: "n", Status: types.AllocDeleted})
			}
		}(i)
	}

	// Reader.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = idx.snapshot()
		}
	}()

	// Run for a brief but non-trivial window so the race detector has
	// time to interleave.
	deadline := make(chan struct{})
	go func() {
		for i := 0; i < 100000; i++ {
		}
		close(deadline)
	}()
	<-deadline
	close(stop)
	wg.Wait()
}
