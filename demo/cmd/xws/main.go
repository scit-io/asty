package main

import (
	"encoding/json"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"asty/demo/internal/xws"
	"asty/demo/middleware"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var validSID = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,64}$`)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	cfg := xws.LoadConfig()

	nc, err := nats.Connect(cfg.NATSUrl)
	if err != nil {
		log.Fatal().Err(err).Msg("NATS connect")
	}
	defer nc.Drain()

	mgr := xws.NewManager(nc, cfg.InactivityTimeout, log.Logger)

	srv, err := micro.AddService(nc, micro.Config{
		Name:    "xws",
		Version: "1.0.0",
	})
	if err != nil {
		log.Fatal().Err(err).Msg("micro.AddService")
	}

	connect := middleware.RequireAuthMicro(cfg.AccessSecret, func(req micro.Request) {
		sid := req.Headers().Get("Sid")
		if sid == "" {
			var body struct {
				SID string `json:"sid"`
			}
			if json.Unmarshal(req.Data(), &body) != nil || body.SID == "" {
				_ = req.Error("400", "missing sid", nil)
				return
			}
			sid = body.SID
		}
		if !validSID.MatchString(sid) {
			log.Warn().Str("sid", sid).Msg("invalid sid format")
			_ = req.Error("400", "invalid sid format", nil)
			return
		}
		mgr.Open(sid)
		_ = req.Respond(nil)
	})

	root := srv.AddGroup("api.v1.xws.ws")
	if err := root.AddEndpoint("connect", connect); err != nil {
		log.Fatal().Err(err).Msg("AddEndpoint connect")
	}

	log.Info().Dur("inactivity_timeout", cfg.InactivityTimeout).Msg("xws started")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig

	_ = srv.Stop()
	mgr.CloseAll()
	log.Info().Msg("xws stopped")
}
