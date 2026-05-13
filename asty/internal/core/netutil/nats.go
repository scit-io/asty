package netutil

import (
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// NATSCreds holds the bits of config that drive a NATS connection.
type NATSCreds struct {
	Host     string
	Port     string
	User     string
	Password string
}

// ConnectNATS opens a NATS connection using the supplied credentials and
// tags it with name (used in NATS server logs to tell agent/server apart).
//
// The caller owns the returned connection and must Close it when done.
func ConnectNATS(creds NATSCreds, name string) (*nats.Conn, error) {
	url := fmt.Sprintf("nats://%s:%s", creds.Host, creds.Port)

	opts := []nats.Option{nats.Name(name)}
	if creds.User != "" {
		opts = append(opts, nats.UserInfo(creds.User, creds.Password))
	}

	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", url, err)
	}

	log.Info().Str("url", url).Msg("connected to NATS")
	return nc, nil
}
