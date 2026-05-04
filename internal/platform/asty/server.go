package asty

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// Server handles scheduling, autoscaling, and orchestration
type Server struct {
	cfg *Config
	nc  *nats.Conn
}

// NewServer creates a new Asty server
func NewServer(cfg *Config) (*Server, error) {
	return &Server{
		cfg: cfg,
	}, nil
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	log.Info().Msg("server starting")

	// Connect to NATS
	if err := s.connectNATS(); err != nil {
		return fmt.Errorf("failed to connect to NATS: %w", err)
	}
	defer s.nc.Close()

	// TODO: implement server lifecycle
	// - Leader election
	// - Scheduling
	// - Autoscaling
	// - Deployment management
	// - HTTP API / Web UI

	<-ctx.Done()
	return nil
}

func (s *Server) connectNATS() error {
	natsURL := fmt.Sprintf("nats://%s:%s", s.cfg.NATSHost, s.cfg.NATSPort)

	opts := []nats.Option{
		nats.Name("asty-server"),
	}

	if s.cfg.NATSUser != "" {
		opts = append(opts, nats.UserInfo(s.cfg.NATSUser, s.cfg.NATSPassword))
	}

	nc, err := nats.Connect(natsURL, opts...)
	if err != nil {
		return err
	}

	s.nc = nc
	log.Info().Str("url", natsURL).Msg("connected to NATS")
	return nil
}
