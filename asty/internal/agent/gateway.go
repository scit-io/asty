package agent

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"asty/asty/internal/core/types"
	"asty/asty/internal/features/deployment"
	"asty/asty/internal/features/gateway"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// shutdownGracePeriod is how long we wait for in-flight HTTP requests
// and WebSocket sessions to drain before forcing the server closed.
const shutdownGracePeriod = 10 * time.Second

// runGateway starts the embedded HTTP gateway in two goroutines: one
// for the request server and one for the Prometheus /metrics endpoint
// on a separate loopback port. Both honour ctx — on cancellation each
// goes through http.Server.Shutdown with shutdownGracePeriod.
//
// The gateway reuses a.nc (the agent's NATS connection), so there is
// no extra connection, drain, or auth path to maintain.
func (a *Agent) runGateway(ctx context.Context) error {
	cfg := a.cfg.Gateway
	if !cfg.Enabled {
		log.Info().Msg("gateway disabled — skipping")
		return nil
	}

	serviceRules := a.collectRateLimitRules()

	gw, err := gateway.New(ctx, a.nc, cfg, serviceRules, log.Logger)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           gw.Handler(),
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		// BaseContext lets each r.Context() observe gateway shutdown,
		// so NATS round-trips and WS handlers cancel without waiting
		// for their own timeouts.
		BaseContext: func(net.Listener) context.Context { return gw.RootContext() },
	}

	go a.serveGateway(ctx, srv)
	if cfg.MetricsAddr != "" {
		go a.serveGatewayMetrics(ctx, cfg.MetricsAddr)
	}
	return nil
}

// collectRateLimitRules loads all .asty files from service_dir and
// collects rate_limit rules declared by each service. Called once at
// gateway startup — rules are static for the agent's lifetime.
func (a *Agent) collectRateLimitRules() []types.RateLimitRule {
	loader := deployment.NewServiceLoader(a.cfg.Agent.ServiceDir)
	services, err := loader.LoadAll()
	if err != nil {
		log.Warn().Err(err).Msg("failed to load service definitions for rate-limit rules")
		return nil
	}
	var rules []types.RateLimitRule
	for _, svc := range services {
		rules = append(rules, svc.RateLimit...)
	}
	return rules
}

// serveGateway runs the HTTP server and shuts it down on ctx cancel.
// A ListenAndServe error other than ErrServerClosed terminates the
// agent — that is the same posture as the controller and command
// subscriptions: a fatal mis-bind should not be hidden behind a log.
func (a *Agent) serveGateway(ctx context.Context, srv *http.Server) {
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("gateway listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			log.Fatal().Err(err).Msg("gateway server failed")
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("gateway shutdown error")
	}
}

// serveGatewayMetrics serves Prometheus /metrics on a loopback port.
// A failure here is logged but does not bring down the agent — losing
// metric scrapes is preferable to losing traffic-serving.
func (a *Agent) serveGatewayMetrics(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", addr).Msg("gateway metrics listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			log.Error().Err(err).Msg("gateway metrics server failed")
		}
		return
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
