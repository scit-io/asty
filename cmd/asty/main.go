package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/upway/asty/internal/platform/asty"
)

func main() {
	// Parse flags
	mode := flag.String("mode", "agent", "Run mode: agent or server")
	flag.Parse()

	// Setup logging
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Info().Msg("shutdown signal received")
		cancel()
	}()

	// Load config
	cfg, err := asty.LoadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
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

func runAgent(ctx context.Context, cfg *asty.Config) {
	log.Info().Msg("starting asty agent")

	agent, err := asty.NewAgent(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create agent")
	}

	if err := agent.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("agent failed")
	}

	<-ctx.Done()
	log.Info().Msg("agent stopped")
}

func runServer(ctx context.Context, cfg *asty.Config) {
	log.Info().Msg("starting asty server")

	server, err := asty.NewServer(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	if err := server.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("server failed")
	}

	<-ctx.Done()
	log.Info().Msg("server stopped")
}
