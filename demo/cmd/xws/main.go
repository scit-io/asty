package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"asty/demo/internal/xws"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var validSID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfg := xws.LoadConfig()

	nc, err := nats.Connect(cfg.NATSUrl)
	if err != nil {
		log.Fatal().Err(err).Msg("NATS connect")
	}
	defer nc.Drain()

	mgr := xws.NewManager(nc, cfg.InactivityTimeout, log.Logger)

	const (
		connectSubject = "api.v1.xws.ws.connect"
		queue          = "xws"
	)

	nc.Subscribe("xws.ping", func(msg *nats.Msg) {
		msg.Respond([]byte("ok"))
	})

	_, err = nc.QueueSubscribe(connectSubject, queue, func(msg *nats.Msg) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Msg("recover")
			}
		}()

		sid := msg.Header.Get("Sid")
		if sid == "" {
			var req struct {
				SID string `json:"sid"`
			}
			if json.Unmarshal(msg.Data, &req) != nil || req.SID == "" {
				return
			}
			sid = req.SID
		}
		if !validSID.MatchString(sid) {
			log.Warn().Str("sid", sid).Msg("invalid sid format")
			return
		}
		mgr.Open(sid)
		msg.Respond(nil)
	})
	if err != nil {
		log.Fatal().Err(err).Msg("QueueSubscribe")
	}

	log.Info().Dur("inactivity_timeout", cfg.InactivityTimeout).Msg("xws started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	mgr.CloseAll()
	log.Info().Msg("xws stopped")
}
