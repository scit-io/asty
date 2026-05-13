package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"asty/asty/internal/agent"
	"asty/asty/internal/core/config"
	"asty/asty/internal/server"
)

func main() {
	mode := flag.String("mode", "agent", "Run mode: agent or server")
	configPath := flag.String("config", "", "Path to config.asty (default: ./config.asty)")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

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
	if err := cfg.Validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid config")
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
