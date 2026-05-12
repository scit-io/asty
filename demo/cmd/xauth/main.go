package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"asty/demo/internal/xauth"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg := xauth.LoadConfig()

	nc, err := nats.Connect(cfg.NATSUrl)
	if err != nil {
		log.Fatal().Err(err).Msg("NATS connect")
	}
	defer nc.Drain()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatal().Err(err).Msg("JetStream init")
	}

	kv, err := js.KeyValue(context.Background(), cfg.KVBucket)
	if err != nil {
		log.Fatal().Err(err).Str("bucket", cfg.KVBucket).Msg("KV bucket not found")
	}

	h := xauth.NewHandlers(kv, cfg, log.Logger)

	srv, err := micro.AddService(nc, micro.Config{
		Name:    "xauth",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatal().Err(err).Msg("micro.AddService")
	}

	root := srv.AddGroup("api.v1.xauth")
	root.AddEndpoint("login", micro.HandlerFunc(h.Login))
	root.AddEndpoint("refresh", micro.HandlerFunc(h.Refresh))
	root.AddEndpoint("logout", micro.HandlerFunc(h.Logout))
	root.AddEndpoint("me", micro.HandlerFunc(h.Me))

	log.Info().
		Str("access_ttl", cfg.AccessTTL.String()).
		Str("refresh_ttl", cfg.RefreshTTL.String()).
		Msg("xauth started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	srv.Stop()
	log.Info().Msg("xauth stopped")
}
