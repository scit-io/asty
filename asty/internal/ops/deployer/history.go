package deployer

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// jsonMarshal aliases encoding/json.Marshal so the publishLast path
// uses the same JSON encoding as drain.progress — deploy.progress and
// drain.progress are intentionally JSON, not CBOR, because SSE clients
// decode JSON natively.
var jsonMarshal = json.Marshal

// historyCap — how many past deployments are kept in memory. Old
// records roll off oldest-first when capacity is reached. Operators
// usually want recent history for debugging; long-term auditing is
// expected to come from logs, not this in-memory ring.
const historyCap = 100

// beginRecord adds a new StateRunning entry to history at the start of a
// deployment.
func (d *Deployer) beginRecord(plan *DeploymentPlan) {
	record := DeploymentRecord{
		ID:        fmt.Sprintf("deploy-%s-%d", plan.ServiceName, time.Now().UnixNano()),
		Service:   plan.ServiceName,
		Version:   plan.TargetVersion,
		Strategy:  "rolling",
		Status:    StateRunning,
		StartedAt: time.Now(),
		Progress:  0,
	}
	if plan.UpdateStrategy.Canary > 0 {
		record.Strategy = "canary"
	}
	d.addRecord(record)
}

func (d *Deployer) addRecord(record DeploymentRecord) {
	d.mu.Lock()
	if len(d.history) >= historyCap {
		d.history = d.history[1:]
	}
	d.history = append(d.history, record)
	d.mu.Unlock()
	d.publishLast()
}

// ApplyRemoteRecord merges a deployment record received via NATS
// (subject asty.v1.deploy.progress.<service>) into the local history
// ring. If a record with the same ID already exists, its fields are
// overwritten so progressing status reflects on every node; otherwise
// the record is appended. Without this every server's GET /deploy
// returned only the locally-initiated runs — followers showed an
// empty history table even though deploys had completed elsewhere.
func (d *Deployer) ApplyRemoteRecord(rec DeploymentRecord) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i := range d.history {
		if d.history[i].ID == rec.ID {
			d.history[i] = rec
			return
		}
	}
	if len(d.history) >= historyCap {
		d.history = d.history[1:]
	}
	d.history = append(d.history, rec)
}

func (d *Deployer) updateLastRecord(status State, progress int) {
	d.mu.Lock()
	if len(d.history) == 0 {
		d.mu.Unlock()
		return
	}
	last := &d.history[len(d.history)-1]
	last.Status = status
	last.Progress = progress
	if status != StateRunning {
		last.CompletedAt = time.Now()
	}
	d.mu.Unlock()
	d.publishLast()
}

// publishLast publishes the most recent DeploymentRecord as JSON on
// `asty.v1.deploy.progress.<service>` so SSE-subscribed dashboards see
// live progress without polling. Best-effort — failure is logged at
// warn, the in-memory ring stays authoritative for the dashboard.
// Asty does not persist deployment history; external log/metric
// shippers own retention.
func (d *Deployer) publishLast() {
	d.mu.Lock()
	if len(d.history) == 0 {
		d.mu.Unlock()
		return
	}
	rec := d.history[len(d.history)-1]
	d.mu.Unlock()

	if d.nc == nil {
		return
	}
	jsonPayload, err := jsonMarshal(&rec)
	if err != nil {
		log.Warn().Err(err).Str("service", rec.Service).Msg("deployment: marshal for NATS publish failed")
		return
	}
	subject := "asty.v1.deploy.progress." + rec.Service
	if err := d.nc.Publish(subject, jsonPayload); err != nil {
		log.Warn().Err(err).Str("subject", subject).Msg("deployment: NATS publish failed")
	}
}

// GetHistory returns deployment history, newest-first. Capped at
// historyCap entries.
func (d *Deployer) GetHistory() []DeploymentRecord {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]DeploymentRecord, len(d.history))
	copy(out, d.history)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
