package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// natsScrapeInterval — how often the agent polls the local NATS
// monitoring endpoints. 5s matches the heartbeat cadence so the values
// surfaced in NodeInfo are at most one heartbeat behind reality.
const natsScrapeInterval = 5 * time.Second

// natsScrapeTimeout — per-request timeout when hitting the monitoring
// port. Local-only HTTP, so anything beyond 2s means the NATS server
// is overloaded or unreachable and we should bail out rather than
// block the scraper goroutine.
const natsScrapeTimeout = 2 * time.Second

// natsStats holds the last successful scrape. Fields are kept JSON-
// addressable so they can be folded into NodeInfo without a copy step.
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

// scrapeNATSLoop runs the polling loop until ctx is cancelled. On
// success it writes into a.natsStats, which getNodeInfo reads under a
// read-lock. Errors are downgraded to debug after the first warning to
// avoid drowning the log when NATS monitoring is unconfigured.
func (a *Agent) scrapeNATSLoop(ctx context.Context) {
	ticker := time.NewTicker(natsScrapeInterval)
	defer ticker.Stop()

	client := &http.Client{Timeout: natsScrapeTimeout}
	base := fmt.Sprintf("http://%s:%s", a.cfg.NATS.Host, a.cfg.NATS.MonitoringPort)
	failures := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.scrapeOnce(client, base); err != nil {
				failures++
				if failures == 1 {
					log.Warn().Err(err).Str("base", base).Msg("NATS monitoring unreachable; nats_* metrics will be zero")
				} else {
					log.Debug().Err(err).Int("failures", failures).Msg("NATS scrape failed")
				}
				continue
			}
			if failures > 0 {
				log.Info().Str("base", base).Msg("NATS monitoring recovered")
				failures = 0
			}
		}
	}
}

func (a *Agent) scrapeOnce(client *http.Client, base string) error {
	var varz struct {
		CPU           float64 `json:"cpu"`
		Mem           int64   `json:"mem"` // bytes
		Connections   int     `json:"connections"`
		Subscriptions int     `json:"subscriptions"`
		SlowConsumers int64   `json:"slow_consumers"`
		InMsgs        int64   `json:"in_msgs"`
		OutMsgs       int64   `json:"out_msgs"`
	}
	if err := fetchJSON(client, base+"/varz", &varz); err != nil {
		return err
	}

	var jsz struct {
		Messages int64 `json:"messages"`
		Bytes    int64 `json:"bytes"`
	}
	if err := fetchJSON(client, base+"/jsz", &jsz); err != nil {
		// JetStream may be disabled — treat as zero, not as a hard failure.
		jsz.Messages, jsz.Bytes = 0, 0
	}

	a.natsStats.mu.Lock()
	defer a.natsStats.mu.Unlock()
	a.natsStats.cpuPercent = varz.CPU
	a.natsStats.memoryMB = varz.Mem / (1024 * 1024)
	a.natsStats.connections = varz.Connections
	a.natsStats.subscriptions = varz.Subscriptions
	a.natsStats.slowConsumers = varz.SlowConsumers
	a.natsStats.inMsgs = varz.InMsgs
	a.natsStats.outMsgs = varz.OutMsgs
	a.natsStats.jetStreamMessages = jsz.Messages
	a.natsStats.jetStreamBytes = jsz.Bytes
	return nil
}

func fetchJSON(client *http.Client, url string, out any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("status %d from %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
