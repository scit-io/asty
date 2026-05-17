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

	"github.com/rs/zerolog/log"
)

// shutdownGracePeriod is how long we wait for in-flight HTTP requests
// and WebSocket sessions to drain before forcing the server closed.
const shutdownGracePeriod = 10 * time.Second

// runGateway starts the embedded HTTP gateway. The single goroutine
// runs the request server and honours ctx — on cancellation it goes
// through http.Server.Shutdown with shutdownGracePeriod.
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

	gw, err := gateway.New(ctx, a.nc, cfg, a.nodeID, serviceRules, log.Logger)
	if err != nil {
		return err
	}

	// RPS reporter feeds the leader's autoscaler with valid-traffic
	// samples for locality-aware scale-up. Bound to the gateway's root
	// context so it exits with the rest of the gateway on shutdown.
	go gw.ReportRPSLoop(gw.RootContext())

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
