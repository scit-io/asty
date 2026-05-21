package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"asty/asty/internal/api/gateway"
	"asty/asty/internal/core/types"
	"asty/asty/internal/ops/deployer"

	"github.com/rs/zerolog/log"
)

// shutdownGracePeriod is how long we wait for in-flight HTTP requests
// and WebSocket sessions to drain before forcing the server closed.
const shutdownGracePeriod = 10 * time.Second

// preBindGateway calls net.Listen on the gateway's address while the
// agent still holds whatever privileges it started with. Returns the
// open listener; the dropPrivileges step that follows shrinks the
// agent down to RunAsUser, and the listener — being an open FD —
// survives the setuid. runGatewayWith then serves on it.
//
// Returns nil (no error) when gateway is disabled, so the caller can
// pass the nil through to runGatewayWith without a branch.
func (a *Agent) preBindGateway() (net.Listener, error) {
	if !a.cfg.Gateway.Enabled {
		return nil, nil
	}
	addr := a.cfg.Gateway.Addr()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	log.Info().Str("addr", listener.Addr().String()).Msg("gateway listener pre-bound")
	return listener, nil
}

// runGatewayWith starts the embedded HTTP gateway serving on the
// pre-bound listener. The single goroutine runs http.Server.Serve
// and honours ctx — on cancellation it goes through Shutdown with
// shutdownGracePeriod.
//
// The gateway reuses a.nc (the agent's NATS connection), so there is
// no extra connection, drain, or auth path to maintain.
func (a *Agent) runGatewayWith(ctx context.Context, listener net.Listener) error {
	cfg := a.cfg.Gateway
	if !cfg.Enabled {
		if listener != nil {
			_ = listener.Close()
		}
		log.Info().Msg("gateway disabled — skipping")
		return nil
	}
	if listener == nil {
		return fmt.Errorf("gateway: no pre-bound listener")
	}

	serviceRules := a.collectRateLimitRules()

	gw, err := gateway.New(ctx, a.nc, cfg, a.nodeID, serviceRules, log.With().Str("component", "gateway").Logger())
	if err != nil {
		_ = listener.Close()
		return err
	}

	// RPS reporter feeds the leader's autoscaler with valid-traffic
	// samples for locality-aware scale-up. Bound to the gateway's root
	// context so it exits with the rest of the gateway on shutdown.
	go gw.ReportRPSLoop(gw.RootContext())

	srv := &http.Server{
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

	go a.serveGateway(ctx, srv, listener)
	return nil
}

// collectRateLimitRules loads all .asty files from service_dir and
// collects rate_limit rules declared by each service. Called once at
// gateway startup — rules are static for the agent's lifetime.
func (a *Agent) collectRateLimitRules() []types.RateLimitRule {
	loader := deployer.NewServiceLoader(a.cfg.Agent.ServiceDir)
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

// serveGateway runs the HTTP server on the pre-bound listener and
// shuts it down on ctx cancel. A Serve error other than ErrServerClosed
// terminates the agent — that is the same posture as the controller
// and command subscriptions: a fatal mis-bind should not be hidden
// behind a log.
func (a *Agent) serveGateway(ctx context.Context, srv *http.Server, listener net.Listener) {
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", listener.Addr().String()).Msg("gateway listening")
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
