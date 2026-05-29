package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"asty/asty/internal/core/natsconf"

	"github.com/rs/zerolog/log"
)

// natsBootstrapTimeout caps how long we wait for the local nats-server
// to start accepting connections after we exec it. The TCP probe runs
// at 100ms intervals; 30s leaves ample margin for cold-start JetStream
// store initialization on a slow disk.
const natsBootstrapTimeout = 30 * time.Second

// natsReadyProbeInterval — how often the TCP probe retries while
// waiting for nats-server to bind its listen socket.
const natsReadyProbeInterval = 100 * time.Millisecond

// bootstrapNATS launches the local nats-server child process from a
// configuration rendered out of cfg.NATS. Blocks until the process is
// accepting connections on the configured listen port. After return,
// agent.natsServerCmd holds the child; superviseNATS owns it from
// there. Called once at startup and again on every cold restart.
//
// We use exec.Command (NOT exec.CommandContext) deliberately:
// CommandContext sends SIGKILL the instant ctx is cancelled, which
// races the orderly-shutdown path in Start (KV.Delete against a
// still-live broker, then close natsStopCh so the supervisor SIGTERMs
// the child cleanly). Killing nats-server out from under the
// deregister would make it time out and the leader's snapshot would
// keep the dead node listed.
func (a *Agent) bootstrapNATS(ctx context.Context) error {
	binary, err := findNATSServerBinary()
	if err != nil {
		return err
	}

	nodeIP := a.resolveNodeIP()
	if nodeIP == "" {
		return fmt.Errorf("cannot resolve node IP for nats-server listen address")
	}

	if err := a.writeNATSConf(a.renderNATSConf(nodeIP)); err != nil {
		return err
	}

	cmd := exec.Command(binary, "-c", a.natsConfPath())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// When drop-root is configured, the nats-server child must start
	// already under the target uid/gid — otherwise the agent (running
	// as asty-svc post-drop) can't signal a root child for SIGHUP
	// (peer-list hot reload) or SIGTERM (clean shutdown). Pre-chown
	// of store_dir in dropPrivileges() complements this so the child
	// can write JetStream files.
	if cred := a.credentialForChildren(); cred != nil {
		cmd.SysProcAttr = withCredential(cmd.SysProcAttr, cred)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start nats-server: %w", err)
	}
	a.natsServerCmd = cmd
	log.Info().Str("binary", binary).Str("conf", a.natsConfPath()).Int("pid", cmd.Process.Pid).Msg("nats-server started")

	addr := fmt.Sprintf("%s:%d", nodeIP, a.cfg.NATS.Server.Port)
	if err := waitForTCP(ctx, addr, natsBootstrapTimeout); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("nats-server not ready on %s: %w", addr, err)
	}
	log.Info().Str("addr", addr).Msg("nats-server ready")
	return nil
}

func (a *Agent) natsConfPath() string {
	return filepath.Join(a.workDir, "nats.conf")
}

func (a *Agent) renderNATSConf(nodeIP string) string {
	return natsconf.Render(natsconf.Input{
		Config: a.cfg.NATS,
		NodeID: a.nodeID,
		NodeIP: nodeIP,
		Peers:  a.resolveNATSPeers(nodeIP),
	})
}

func (a *Agent) writeNATSConf(content string) error {
	path := a.natsConfPath()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// resolveNodeIP picks the address nats-server should bind to: explicit
// NodeIP wins, otherwise we fall back to LocalIPv4 (best effort). The
// same value is what other nodes will use to reach this NATS via the
// rendered cluster.routes on their side.
func (a *Agent) resolveNodeIP() string {
	if a.cfg.NodeIP != "" {
		return a.cfg.NodeIP
	}
	if ifaces, _ := net.InterfaceAddrs(); len(ifaces) > 0 {
		for _, addr := range ifaces {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() || ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String()
		}
	}
	return ""
}

// Peer resolution helpers (resolveNATSPeers, filterSelf) live in
// natspeers.go.

// findNATSServerBinary locates the nats-server binary the supervisor
// exec's. Order: same directory as the running asty binary (handles the
// `make nats-server` layout), then $PATH.
func findNATSServerBinary() (string, error) {
	if astyPath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(astyPath), "nats-server")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	p, err := exec.LookPath("nats-server")
	if err != nil {
		return "", fmt.Errorf("nats-server not found (looked next to asty binary and in $PATH): %w", err)
	}
	return p, nil
}

// waitForTCP retries DialTimeout until success, ctx, or timeout.
func waitForTCP(ctx context.Context, addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s", timeout)
		}
		conn, err := net.DialTimeout("tcp", addr, natsReadyProbeInterval)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(natsReadyProbeInterval):
		}
	}
}
