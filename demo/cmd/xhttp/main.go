package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"
	"time"

	"asty/demo/internal/xhttp"

	_ "github.com/lib/pq"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg := xhttp.LoadConfig()

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("sql.Open")
	}
	defer db.Close()

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("db.Ping")
	}
	if err := xhttp.Migrate(db); err != nil {
		log.Fatal().Err(err).Msg("migrate")
	}
	log.Info().Msg("PostgreSQL ready")

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

	h := xhttp.NewHandlers(kv, db, cfg, log.Logger)

	srv, err := micro.AddService(nc, micro.Config{
		Name:    "xhttp",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatal().Err(err).Msg("micro.AddService")
	}

	root := srv.AddGroup("api.v1.xhttp")
	root.AddEndpoint("create", micro.HandlerFunc(h.Create))
	root.AddEndpoint("get", micro.HandlerFunc(h.Get))
	root.AddEndpoint("list", micro.HandlerFunc(h.List))
	root.AddEndpoint("update", micro.HandlerFunc(h.Update))
	root.AddEndpoint("delete", micro.HandlerFunc(h.Delete))

	log.Info().Msg("xhttp started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	srv.Stop()
	log.Info().Msg("xhttp stopped")
}
