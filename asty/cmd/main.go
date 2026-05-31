package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"

	"asty/asty/internal/agent"
	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/config"
	"asty/asty/internal/core/natsconf"
	"asty/asty/internal/core/netutil"
	"asty/asty/internal/core/types"
	"asty/asty/internal/infra/logs"
	"asty/asty/internal/server"
)

func main() {
	mode := flag.String("mode", "agent", "Run mode: agent, server, nats-conf, or admin")
	configPath := flag.String("config", "", "Path to config.asty (default: ./config.asty)")
	peersFlag := flag.String("peers", "", "Comma-separated peer IPs (nats-conf mode only)")
	flag.Parse()

	logs.InitGlobal(*mode)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Info().Msg("shutdown signal received")
		cancel()
	}()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// nats-conf only renders the NATS server config; it skips top-level
	// validation (token) so it can be run against a partial dev config
	// without forcing the operator to set every prod-required key.
	if *mode == "nats-conf" {
		if err := cfg.NATS.Validate(); err != nil {
			log.Fatal().Err(err).Msg("invalid nats config")
		}
		renderNATSConf(cfg, *peersFlag)
		return
	}

	// admin subcommands talk to the local NATS using nats creds, not the
	// dashboard token — skip the top-level Validate (which requires
	// A_TOKEN) but still enforce NATS-section sanity.
	if *mode == "admin" {
		if err := cfg.NATS.Validate(); err != nil {
			log.Fatal().Err(err).Msg("invalid nats config")
		}
		runAdmin(cfg, flag.Args())
		return
	}

	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid config")
	}
	logs.SetLevel(cfg.LogLevel)

	// Dev mode flips both codec.Wire and codec.State back to JSON so
	// `nats sub`/`nats kv get` show readable payloads. Production keeps
	// the CBOR defaults.
	if cfg.DevMode {
		codec.UseJSONForDev()
		log.Info().Msg("dev_mode: codec.Wire and codec.State on JSON")
	}

	switch *mode {
	case "agent":
		runAgent(ctx, cfg)
	case "server":
		runServer(ctx, cfg)
	default:
		log.Fatal().Str("mode", *mode).Msg("unknown mode")
	}
}

func runAgent(ctx context.Context, cfg *config.Config) {
	log.Info().Msg("starting asty agent")

	a, err := agent.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create agent")
	}

	// Start blocks until shutdown completes — on SIGTERM (parent ctx) or
	// on a dashboard kill, which cancels Start's own derived ctx via
	// CmdShutdown. It returns only after the graceful teardown has run,
	// so we just exit here. (Waiting on ctx.Done() afterwards would hang
	// the process forever on the kill path, where the parent ctx is
	// still live — that left zombie agents behind a dashboard kill.)
	if err := a.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("agent failed")
	}
	log.Info().Msg("agent stopped")
}

func runServer(ctx context.Context, cfg *config.Config) {
	log.Info().Msg("starting asty server")

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	// Start blocks until shutdown completes — on SIGTERM (parent ctx) or
	// on self-removal, where watchSelfRemoval cancels Start's own derived
	// ctx after the node's KV entry is deleted. It returns only after the
	// graceful teardown has run, so we just exit here. (Waiting on
	// ctx.Done() afterwards would hang forever on the self-removal path,
	// where the parent ctx is still live — that left zombie servers
	// behind a dashboard kill.)
	if err := srv.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}
	log.Info().Msg("server stopped")
}

// runAdmin dispatches `asty -mode admin <subcommand>`. The admin path
// is the operator-side companion of the agent: invoked over SSH
// against a running node to perform privileged setup that the
// network-exposed dashboard intentionally does NOT cover. Today it
// has one subcommand — `add-peer` — used during cluster growth.
func runAdmin(cfg *config.Config, args []string) {
	if len(args) < 1 {
		log.Fatal().Msg("usage: asty -mode admin <subcommand>  (subcommands: add-peer)")
	}
	switch sub := args[0]; sub {
	case "add-peer":
		runAdminAddPeer(cfg)
	default:
		log.Fatal().Str("subcommand", sub).Msg("unknown admin subcommand")
	}
}

// runAdminAddPeer connects to the LOCAL nats-server with the agent's
// own credentials and publishes a CmdAddPeer to the local agent. The
// agent records the IP in its bootstrap-peer set and SIGHUPs (or
// cold-restarts) nats-server so cluster.routes opens up for the
// incoming join. Designed to be the body of an `authorized_keys`
// command=" ..." entry — the SSH key restriction is what gates access.
//
// The peer IP comes from $SSH_CLIENT (first whitespace-separated
// field), set by sshd on the receiving side. This is the only input
// channel — no flag — so an attacker cannot lie about the IP by
// passing a forged argument. Local invocations (dev tests) simulate
// it by setting SSH_CLIENT in the environment.
func runAdminAddPeer(cfg *config.Config) {
	var ip string
	if v := os.Getenv("SSH_CLIENT"); v != "" {
		if f := strings.Fields(v); len(f) > 0 {
			ip = f[0]
		}
	}
	if ip == "" {
		log.Fatal().Msg("add-peer: $SSH_CLIENT is empty — invoke via SSH (or set SSH_CLIENT=<ip> ... in local tests)")
	}
	if cfg.NodeID == "" {
		log.Fatal().Msg("add-peer: node_id is required (set via config.asty or A_NODE_ID)")
	}

	host := cfg.NodeIP
	if host == "" {
		host = netutil.LocalIPv4("")
	}
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: host, Port: cfg.NATS.Server.Port,
		User: cfg.NATS.User, Password: cfg.NATS.Password,
	}, "asty-admin-add-peer")
	if err != nil {
		log.Fatal().Err(err).Msg("add-peer: connect to local NATS failed")
	}
	defer nc.Close()

	payload, err := types.MarshalAddPeerCommand(ip)
	if err != nil {
		log.Fatal().Err(err).Msg("add-peer: marshal failed")
	}
	subject := types.CommandSubject(cfg.NodeID, types.CmdAddPeer)
	reply, err := nc.Request(subject, payload, 5*time.Second)
	if err != nil {
		log.Fatal().Err(err).Str("subject", subject).Msg("add-peer: NATS request failed")
	}
	var resp types.CommandResponse
	if err := codec.Wire.Unmarshal(reply.Data, &resp); err != nil {
		log.Fatal().Err(err).Msg("add-peer: parse response failed")
	}
	if !resp.Success {
		log.Fatal().Str("error", resp.Error).Msg("add-peer: agent rejected")
	}
	fmt.Printf("add-peer ok: %s (ip=%s)\n", resp.Message, ip)
}

// renderNATSConf prints to stdout the nats-server configuration that
// the agent would write at startup for this node. NodeID and NodeIP
// come from the loaded config (which already absorbed env overrides).
// Peers must be supplied via -peers because this subcommand has no
// access to the cluster KV the agent normally watches — its purpose
// is offline inspection.
func renderNATSConf(cfg *config.Config, peers string) {
	if cfg.NodeID == "" {
		log.Fatal().Msg("node_id is required (set via config.asty or A_NODE_ID)")
	}
	if cfg.NodeIP == "" {
		log.Fatal().Msg("node_ip is required (set via config.asty or A_NODE_IP)")
	}

	var peerList []string
	for _, p := range strings.Split(peers, ",") {
		if s := strings.TrimSpace(p); s != "" {
			peerList = append(peerList, s)
		}
	}

	out := natsconf.Render(natsconf.Input{
		Config: cfg.NATS,
		NodeID: cfg.NodeID,
		NodeIP: cfg.NodeIP,
		Peers:  peerList,
	})
	fmt.Print(out)
}
