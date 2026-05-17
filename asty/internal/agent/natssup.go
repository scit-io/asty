package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
// agent.natsServerCmd holds the child; agent.waitForNATSExit must run
// in a goroutine to surface child-death.
func (a *Agent) bootstrapNATS(ctx context.Context) error {
	binary, err := findNATSServerBinary()
	if err != nil {
		return err
	}

	nodeIP := a.resolveNodeIP()
	if nodeIP == "" {
		return fmt.Errorf("cannot resolve node IP for nats-server listen address")
	}

	conf := natsconf.Render(natsconf.Input{
		Config: a.cfg.NATS,
		NodeID: a.nodeID,
		NodeIP: nodeIP,
		Peers:  a.resolveNATSPeers(nodeIP),
	})

	confPath := filepath.Join(a.workDir, "nats.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", confPath, err)
	}

	cmd := exec.CommandContext(ctx, binary, "-c", confPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start nats-server: %w", err)
	}
	a.natsServerCmd = cmd
	log.Info().Str("binary", binary).Str("conf", confPath).Int("pid", cmd.Process.Pid).Msg("nats-server started")

	addr := fmt.Sprintf("%s:%d", nodeIP, a.cfg.NATS.Server.Port)
	if err := waitForTCP(ctx, addr, natsBootstrapTimeout); err != nil {
		_ = cmd.Process.Kill()
		return fmt.Errorf("nats-server not ready on %s: %w", addr, err)
	}
	log.Info().Str("addr", addr).Msg("nats-server ready")
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

// resolveNATSPeers returns the IPs of OTHER cluster nodes for inclusion
// in cluster.routes. A_NATS_PEERS (comma-separated) wins — start.sh
// uses this in dev to wire the loopback aliases. Otherwise we resolve
// cfg.Domain via DNS (matches the discovery package). The local IP is
// filtered out so a node never routes to itself.
func (a *Agent) resolveNATSPeers(selfIP string) []string {
	// A_NATS_PEERS_FILE wins — it's how dev imitates a DNS A-record:
	// start.sh maintains the file, agents re-read it on every watcher
	// tick. In prod the DNS branch below covers the same role.
	if path := os.Getenv("A_NATS_PEERS_FILE"); path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			return filterSelf(splitAndTrim(string(raw)), selfIP)
		}
	}
	if raw := os.Getenv("A_NATS_PEERS"); raw != "" {
		return filterSelf(splitAndTrim(raw), selfIP)
	}
	if a.cfg.Domain == "" {
		return nil
	}
	ips, err := net.LookupIP(a.cfg.Domain)
	if err != nil {
		log.Warn().Err(err).Str("domain", a.cfg.Domain).Msg("NATS peer discovery: DNS lookup failed")
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip.To4() == nil {
			continue
		}
		out = append(out, ip.String())
	}
	return filterSelf(out, selfIP)
}

// splitAndTrim splits on commas AND any whitespace (incl. newlines)
// so the same parser handles both env-var ("a,b,c") and peers-file
// ("a\nb\nc") forms.
func splitAndTrim(raw string) []string {
	out := make([]string, 0)
	for _, p := range strings.FieldsFunc(raw, splitPeerSep) {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func splitPeerSep(r rune) bool {
	return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
}

func filterSelf(ips []string, self string) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != self {
			out = append(out, ip)
		}
	}
	return out
}

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

// waitForTCP retries connecting to addr until success, ctx cancels, or
// the timeout fires.
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
