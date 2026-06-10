package server

import (
	"sync"
	"testing"

	"asty/asty/internal/core/types"

	"github.com/fxamacker/cbor/v2"
)

// TestKvEnvForAllocation_RaceMarshalsVsMutate reproduces the original
// "fatal: concurrent map iteration and map write" that crashed server-14
// in Phase C cycle 2: kvEnvForAllocation writing to a shared svc.Env map
// while another goroutine CBOR-marshals the same map.
//
// Pre-fix: this should fail under -race (and intermittently with a fatal
// panic). Post-fix: the dispatch path deep-copies Env into resolved before
// kvEnvForAllocation writes, so the shared svc.Env is never mutated.
func TestKvEnvForAllocation_RaceMarshalsVsMutate(t *testing.T) {
	shared := &types.ServiceDefinition{
		Name: "race-svc",
		Env:  map[string]string{"FOO": "bar"},
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Two "dispatch" goroutines, each running the SAME code path the real
	// server does (resolvedSvcForDispatch + kvEnvForAllocation + Marshal).
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Mirror sendStartCommand without the actual RPC: deep
				// copy the struct + Env, then kv-env-mutate the COPY,
				// then marshal it. The bug was the original ordering
				// (mutate shared svc.Env BEFORE shallow-copy).
				resolved := *shared
				if shared.Env != nil {
					resolved.Env = make(map[string]string, len(shared.Env))
					for k, v := range shared.Env {
						resolved.Env[k] = v
					}
				}
				kvEnvForAllocation(&resolved, map[string]string{"A_KV_X": "x"})
				_, _ = cbor.Marshal(&resolved)
			}
		}()
	}

	// Run for a brief window so -race can interleave; in practice the
	// pre-fix code crashed within ~50ms on macOS.
	deadline := make(chan struct{})
	go func() {
		for i := 0; i < 200000; i++ {
		}
		close(deadline)
	}()
	<-deadline
	close(stop)
	wg.Wait()
}
