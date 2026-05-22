package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Deploy streams deploy-progress events for a single service. The
// underlying NATS subject `asty.v1.deploy.progress.<svc>` is fanned
// out by streamHub.SubscribeDeploy; this handler filters to the
// requested service by reading the record's `service` field and
// drops anything published for a different one.
//
// Each event is forwarded verbatim as the SSE payload under the
// `progress` event name. Clients receive the same DeploymentRecord
// JSON that PutDeployment writes to KV — id, status, progress,
// rollback_steps included — so the dashboard can render history-
// equivalent detail live.
func Deploy(ctx Context, w http.ResponseWriter, r *http.Request, serviceName string) {
	if serviceName == "" {
		http.Error(w, "service name required", http.StatusBadRequest)
		return
	}
	flusher := Setup(w)
	if flusher == nil {
		return
	}

	progress, unsub := ctx.StreamHub().SubscribeDeploy()
	defer unsub()

	ping := time.NewTicker(ssePingInterval)
	defer ping.Stop()

	rctx := r.Context()
	for {
		select {
		case <-rctx.Done():
			return
		case data, ok := <-progress:
			if !ok {
				return
			}
			if !matchesService(data, serviceName) {
				continue
			}
			Event(w, "progress", data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// matchesService peeks at the JSON payload's `service` field without
// fully decoding the record. Returns true when the field matches name
// — the deployer encodes one service per event so the test is exact.
func matchesService(data []byte, name string) bool {
	var hdr struct {
		Service string `json:"service"`
	}
	if err := json.Unmarshal(data, &hdr); err != nil {
		return false
	}
	return hdr.Service == name
}
