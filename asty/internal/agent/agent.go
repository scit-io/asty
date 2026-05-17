package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"asty/asty/internal/core/config"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/features/clustering/state"
	"asty/asty/internal/features/deployment/artifacts"
	"asty/asty/internal/features/execution/health"
	"asty/asty/internal/features/execution/process"
	"asty/asty/internal/features/observability/logs"
	"asty/asty/internal/features/observability/metrics"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
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

	healthChecker      *health.Checker
	metricsCollector   *metrics.Collector
	artifactDownloader *artifacts.Downloader
	clusterState       *state.ClusterState
	natsStats          natsStats
	natsServerCmd      *exec.Cmd

	// natsRestartCh fires when watchNATSPeers detects a change in the
	// peer list and the supervisor must rebuild nats.conf and restart
	// the broker. Buffer 1 — a second tick before the supervisor
	// services the first is harmless: the restart already picks up the
	// freshest peer list.
	natsRestartCh chan struct{}

	workDir string

	// failed receives service names whose process exited unexpectedly.
	// Populated by per-process OnExit callbacks; drained by
	// monitorProcesses, which decides whether to restart or give up.
	failed chan string
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
		artifactDownloader: artifacts.NewDownloader(),
		workDir:            workDir,
		failed:             make(chan string, failedServicesBufferSize),
		natsRestartCh:      make(chan struct{}, 1),
	}, nil
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

// Start brings up the agent: NATS connection, cluster state, log
// forwarding, health/metrics collectors, command subscriptions, and the
// background goroutines for heartbeats, metrics publishing, and process
// monitoring. Blocks until ctx is cancelled, then stops all processes.
func (a *Agent) Start(ctx context.Context) error {
	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent starting")

	a.exportConfigEnv()

	if err := a.bootstrapNATS(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap NATS: %w", err)
	}
	go a.superviseNATS(ctx)
	go a.watchNATSPeers(ctx)

	host := a.cfg.NodeIP
	if host == "" {
		host = netutil.LocalIPv4("")
	}
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: host, Port: a.cfg.NATS.Server.Port,
		User: a.cfg.NATS.User, Password: a.cfg.NATS.Password,
	}, "asty-agent-"+a.nodeID)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	a.nc = nc
	defer a.nc.Close()

	// Separate connection in the SYS account, used exclusively by
	// natsstats.go for $SYS.REQ.SERVER.*.STATSZ/JSZ. Optional: if no
	// observer credentials are configured the agent still comes up and
	// the asty_node_nats_* metrics simply stay at zero.
	if a.cfg.NATS.ObserverUser != "" {
		ncSys, err := netutil.ConnectNATS(netutil.NATSCreds{
			Host: host, Port: a.cfg.NATS.Server.Port,
			User: a.cfg.NATS.ObserverUser, Password: a.cfg.NATS.ObserverPassword,
		}, "asty-observer-"+a.nodeID)
		if err != nil {
			return fmt.Errorf("failed to connect to NATS as observer: %w", err)
		}
		a.ncSys = ncSys
		defer a.ncSys.Close()
	}

	clusterState, err := state.New(a.nc)
	if err != nil {
		return fmt.Errorf("failed to initialize cluster state: %w", err)
	}
	a.clusterState = clusterState

	a.healthChecker = health.NewChecker(a.nc)

	agentSubject := fmt.Sprintf("asty.v1.agent.%s.logs.agent", a.nodeID)
	natsWriter := logs.NewNATSWriter(a.nc, agentSubject)
	log.Logger = log.Output(io.MultiWriter(log.Logger, natsWriter))

	go a.healthChecker.Start(ctx)
	go a.metricsCollector.Start(ctx)
	a.metricsCollector.Register(os.Getpid(), "asty-agent")

	if err := a.subscribeCommands(); err != nil {
		return fmt.Errorf("failed to subscribe to commands: %w", err)
	}
	if err := a.subscribePing(); err != nil {
		return fmt.Errorf("failed to subscribe to ping: %w", err)
	}

	go a.publishHeartbeat(ctx)
	go a.publishProcessMetrics(ctx)
	go a.monitorProcesses(ctx)
	go a.collectNATSStatsLoop(ctx)

	if err := a.runGateway(ctx); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

	log.Info().
		Str("node_id", a.nodeID).
		Str("datacenter", a.cfg.Datacenter).
		Msg("agent ready")

	<-ctx.Done()
	// Best-effort deregister: drop our node entry from the cluster KV
	// so the leader's snapshot (and the asty_node_* metrics it feeds)
	// stops reporting us on the next watcher tick. NATS-graceful path —
	// the supervisor hasn't SIGTERM'd nats-server yet, so the KV write
	// still has a working broker on the same loopback.
	if a.clusterState != nil {
		if err := a.clusterState.RemoveNode(a.nodeID); err != nil {
			log.Warn().Err(err).Msg("graceful shutdown: failed to deregister node from cluster state")
		}
	}
	a.stopAllProcesses()
	return nil
}
