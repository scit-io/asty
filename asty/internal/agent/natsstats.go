package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// natsStatsInterval — how often the agent samples local NATS server
// stats via $SYS request/reply. 5s matches the heartbeat cadence so
// values surfaced in NodeInfo are at most one heartbeat behind reality.
const natsStatsInterval = 5 * time.Second

// natsStatsTimeout caps a single STATSZ/JSZ request. Local NATS, so
// anything over 2s means the server is overloaded or our connection
// dropped — log once and try again next tick.
const natsStatsTimeout = 2 * time.Second

// natsStats holds the last sample of local NATS server stats. Writers:
// collectNATSStatsLoop. Reader: getNodeInfo (under RLock).
type natsStats struct {
	mu sync.RWMutex

	cpuPercent        float64
	memoryMB          int64
	connections       int
	subscriptions     int
	slowConsumers     int64
	inMsgs            int64
	outMsgs           int64
	jetStreamMessages int64
	jetStreamBytes    int64
}

// statszEnvelope matches the JSON shape published by nats-server on
// `$SYS.SERVER.<id>.STATSZ` (and returned by REQ.SERVER.<id>.STATSZ).
// Only the fields used by NodeInfo are decoded; everything else is
// silently dropped.
type statszEnvelope struct {
	Statsz struct {
		Mem           int64   `json:"mem"`
		CPU           float64 `json:"cpu"`
		Connections   int     `json:"connections"`
		Subscriptions int     `json:"subscriptions"`
		SlowConsumers int64   `json:"slow_consumers"`
		Sent          struct {
			Msgs  int64 `json:"msgs"`
			Bytes int64 `json:"bytes"`
		} `json:"sent"`
		Received struct {
			Msgs  int64 `json:"msgs"`
			Bytes int64 `json:"bytes"`
		} `json:"received"`
	} `json:"statsz"`
}

// jszEnvelope matches the JSON shape returned by REQ.SERVER.<id>.JSZ.
type jszEnvelope struct {
	Data struct {
		Messages int64 `json:"messages"`
		Bytes    int64 `json:"bytes"`
	} `json:"data"`
}

// collectNATSStatsLoop polls the local NATS server's $SYS stats every
// natsStatsInterval and updates a.natsStats. Runs until ctx cancels.
// Uses the dedicated SYS-account connection (a.ncSys) — the agent's
// main connection sits in ASTY and cannot read $SYS subjects.
func (a *Agent) collectNATSStatsLoop(ctx context.Context) {
	if a.ncSys == nil {
		log.Warn().Msg("observer NATS connection not configured; asty_node_nats_* metrics will stay zero")
		return
	}
	serverID := a.ncSys.ConnectedServerId()
	if serverID == "" {
		log.Warn().Msg("observer NATS connection has no server id; nats_* metrics will stay zero")
		return
	}
	statszSubj := fmt.Sprintf("$SYS.REQ.SERVER.%s.STATSZ", serverID)
	jszSubj := fmt.Sprintf("$SYS.REQ.SERVER.%s.JSZ", serverID)

	ticker := time.NewTicker(natsStatsInterval)
	defer ticker.Stop()

	warned := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.collectNATSStatsOnce(statszSubj, jszSubj); err != nil {
				if !warned {
					log.Warn().Err(err).Msg("$SYS stats collection failed; check NATS system_account configuration")
					warned = true
				}
				continue
			}
			warned = false
		}
	}
}

func (a *Agent) collectNATSStatsOnce(statszSubj, jszSubj string) error {
	msg, err := a.ncSys.Request(statszSubj, nil, natsStatsTimeout)
	if err != nil {
		return fmt.Errorf("STATSZ request: %w", err)
	}
	var st statszEnvelope
	if err := json.Unmarshal(msg.Data, &st); err != nil {
		return fmt.Errorf("STATSZ decode: %w", err)
	}

	a.natsStats.mu.Lock()
	a.natsStats.cpuPercent = st.Statsz.CPU
	a.natsStats.memoryMB = st.Statsz.Mem / (1024 * 1024)
	a.natsStats.connections = st.Statsz.Connections
	a.natsStats.subscriptions = st.Statsz.Subscriptions
	a.natsStats.slowConsumers = st.Statsz.SlowConsumers
	a.natsStats.inMsgs = st.Statsz.Received.Msgs
	a.natsStats.outMsgs = st.Statsz.Sent.Msgs
	a.natsStats.mu.Unlock()

	// JSZ is best-effort — JetStream may be disabled on this server,
	// in which case the request fails or the payload is empty.
	if msg, err := a.ncSys.Request(jszSubj, nil, natsStatsTimeout); err == nil {
		var js jszEnvelope
		if err := json.Unmarshal(msg.Data, &js); err == nil {
			a.natsStats.mu.Lock()
			a.natsStats.jetStreamMessages = js.Data.Messages
			a.natsStats.jetStreamBytes = js.Data.Bytes
			a.natsStats.mu.Unlock()
		}
	}
	return nil
}
