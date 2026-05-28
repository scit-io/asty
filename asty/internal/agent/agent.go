package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/artifact"
	"asty/asty/internal/infra/kv"
	"asty/asty/internal/infra/metrics"
	"asty/asty/internal/infra/probe"
	"asty/asty/internal/infra/process"

	"github.com/nats-io/nats.go"
)

// defaultWorkRoot is where the agent stores per-node working directories
// when A_WORK_DIR is unset. Each node gets its own subdir so multiple
// agents can run side-by-side on a dev box without colliding.
const defaultWorkRoot = "/var/lib/asty"

// failedServicesBufferSize bounds the channel that carries
// "process exited unexpectedly" notifications from the per-process
// OnExit callbacks to the restart goroutine. A burst larger than this
// would mean many simultaneous failures — we drop in that case rather
// than back-pressure the OnExit callback (which runs on the process
// monitor goroutine and must not block).
const failedServicesBufferSize = 64

// Agent manages processes on a single node.
type Agent struct {
	cfg    *config.Config
	nc     *nats.Conn // ASTY account — cluster KV, asty.v1.*, gateway, logs
	ncSys  *nats.Conn // SYS account — STATSZ/JSZ only; nil if observer creds not configured
	nodeID string

	processes map[string]*process.Process
	mu        sync.RWMutex

	healthChecker      *probe.Checker
	metricsCollector   *metrics.Collector
	artifactDownloader *artifact.Downloader
	clusterState       *kv.ClusterState
	natsStats          natsStats
	natsServerCmd      *exec.Cmd

	// natsRestartCh fires when watchNATSPeers detects a change in the
	// peer list and the supervisor must rebuild nats.conf and restart
	// the broker. Buffer 1 — a second tick before the supervisor
	// services the first is harmless: the restart already picks up the
	// freshest peer list.
	natsRestartCh chan struct{}

	// natsStopCh tells superviseNATS to SIGTERM the nats-server child
	// and exit. Closed by Start *after* the graceful deregister so the
	// KV.Delete round-trip still has a live broker on the same loopback;
	// closing on ctx.Done() directly would race the deregister and the
	// write would time out. A sync.Once guards the close so an early-
	// error return path doesn't double-close.
	natsStopCh   chan struct{}
	natsStopOnce sync.Once

	workDir string

	// failed receives service names whose process exited unexpectedly.
	// Populated by per-process OnExit callbacks; drained by
	// monitorProcesses, which decides whether to restart or give up.
	failed chan string

	// drop is the resolved run-as uid/gid pair the agent (and every
	// child it spawns) will run as after dropPrivileges. Zero value
	// when cfg.Agent.RunAsUser is unset — agent stays at the OS uid.
	// Resolved once at Start so a typo in the user name fails fast.
	drop dropTarget

	// shutdownFn cancels Start's derived ctx so CmdShutdown can
	// trigger the same graceful path as SIGTERM.
	shutdownFn context.CancelFunc

	// lastOperatorStatus caches the most recent operator-set status
	// observed in KV (Draining, Drained, Paused). The heartbeat uses
	// it as a fallback when its KV read fails — without it, transient
	// catchup gaps during cluster growth would let the next write
	// silently clobber an operator-set status with default Ready.
	// Reset to "" whenever a successful read returns Ready/Joining
	// (the operator explicitly cleared the drain or the node is just
	// coming up).
	lastOperatorStatus types.NodeStatus
}

// New creates a new Asty agent. The work directory is created on disk
// so subsequent StartService calls can drop binaries into it without
// extra checks.
func New(cfg *config.Config) (*Agent, error) {
	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = netutil.Hostname()
	}

	workDir := filepath.Join(cfg.Agent.WorkDir, nodeID)
	if workDir == nodeID {
		workDir = filepath.Join(defaultWorkRoot, nodeID)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create work directory: %w", err)
	}

	return &Agent{
		cfg:                cfg,
		nodeID:             nodeID,
		processes:          make(map[string]*process.Process),
		metricsCollector:   metrics.NewCollector(cfg.Autoscale.EvalInterval),
		artifactDownloader: artifact.NewDownloader(),
		workDir:            workDir,
		failed:             make(chan string, failedServicesBufferSize),
		natsRestartCh:      make(chan struct{}, 1),
		natsStopCh:         make(chan struct{}),
	}, nil
}

// stopNATSSupervisor signals superviseNATS to terminate the nats-server
// child and exit. Idempotent — Start may call it from the orderly
// shutdown path, but a deferred call from an error return is safe too.
func (a *Agent) stopNATSSupervisor() {
	a.natsStopOnce.Do(func() { close(a.natsStopCh) })
}

// exportConfigEnv ensures the agent's resolved NATS and logging settings
// are present in os.Environ(). Child processes inherit env from the agent
// (via process.Start), so this guarantees services see correct A_* values
// regardless of how the agent itself was configured (YAML, env, defaults).
func (a *Agent) exportConfigEnv() {
	setIfNonEmpty := func(key, val string) {
		if val != "" {
			os.Setenv(key, val)
		}
	}
	host := a.cfg.NodeIP
	if host == "" {
		host = netutil.LocalIPv4("")
	}
	// Spawned services get APP credentials. No fallback to the agent's
	// own user: that user has JetStream KV access to the asty-cluster
	// bucket, and a spawned service inheriting it would be able to
	// rewrite cluster state. If app creds aren't configured, leave
	// A_NATS_USER/PASSWORD unset so services fail loudly at startup.
	user, password := a.cfg.NATS.AppCredentials()
	setIfNonEmpty("A_NATS_HOST", host)
	setIfNonEmpty("A_NATS_PORT", strconv.Itoa(a.cfg.NATS.Server.Port))
	setIfNonEmpty("A_NATS_USER", user)
	setIfNonEmpty("A_NATS_PASSWORD", password)
	setIfNonEmpty("A_LOG_LEVEL", a.cfg.LogLevel)
}

// Start is defined in start.go alongside the NATS-wiring helper it
// delegates to; keeping it out of this file keeps agent.go focused on
// the Agent struct, its construction, and the env-export helper.
