package server

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

// runStandalonePrometheus spawns a second http.Server for the
// Prometheus exposition when cfg.Prometheus.Port differs from
// cfg.Dashboard.Port. The shared-listener path is the default; this
// branch only kicks in when an operator deliberately separates them.
// Lives in its own file so boot.go stays focused on lifecycle wiring.
func (s *Server) runStandalonePrometheus(ctx context.Context) {
	addr := s.cfg.Prometheus.Addr()
	mux := http.NewServeMux()
	mux.Handle("GET "+s.cfg.Prometheus.Prefix, s.httpAPI.PrometheusHandler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Info().Str("addr", addr).Str("prefix", s.cfg.Prometheus.Prefix).Msg("standalone Prometheus listener")
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("standalone Prometheus listener failed")
	}
}
