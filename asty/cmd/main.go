package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"

	"asty/asty/internal/agent"
	"asty/asty/internal/core/codec"
	"asty/asty/internal/core/config"
	"asty/asty/internal/core/natsconf"
	"asty/asty/internal/infra/logs"
	"asty/asty/internal/server"
)

func main() {
	mode := flag.String("mode", "agent", "Run mode: agent, server, or nats-conf")
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
	// validation (domain/token) so it can be run against a partial dev
	// config without forcing the operator to set every prod-required key.
	if *mode == "nats-conf" {
		if err := cfg.NATS.Validate(); err != nil {
			log.Fatal().Err(err).Msg("invalid nats config")
		}
		renderNATSConf(cfg, *peersFlag)
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

	if err := a.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("agent failed")
	}

	<-ctx.Done()
	log.Info().Msg("agent stopped")
}

func runServer(ctx context.Context, cfg *config.Config) {
	log.Info().Msg("starting asty server")

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	if err := srv.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}

	<-ctx.Done()
	log.Info().Msg("server stopped")
}

// renderNATSConf prints to stdout the nats-server configuration that
// the agent would write at startup for this node. NodeID and NodeIP
// come from the loaded config (which already absorbed env overrides).
// Peers must be supplied via -peers because this subcommand does not
// run DNS discovery — its purpose is offline inspection.
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
