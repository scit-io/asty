package netutil

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog/log"
)

// natsConnectTimeout caps how long ConnectNATS will keep retrying before
// it gives up. The local nats-server is brought up by the agent on each
// node — server and agent boot in parallel, so the server may race the
// agent and need to wait for the broker to bind its listen port.
const natsConnectTimeout = 30 * time.Second

// natsConnectRetry is the gap between connection attempts while the
// broker is not yet accepting connections.
const natsConnectRetry = 500 * time.Millisecond

// NATSCreds holds the bits of config that drive a NATS connection.
type NATSCreds struct {
	Host     string
	Port     int
	User     string
	Password string
}

// ConnectNATS opens a NATS connection using the supplied credentials and
// tags it with name (used in NATS server logs to tell agent/server apart).
// It retries on connection failure until natsConnectTimeout elapses so a
// peer process (agent supervises NATS) can finish bootstrapping the
// broker without taking the caller down.
//
// The caller owns the returned connection and must Close it when done.
func ConnectNATS(creds NATSCreds, name string) (*nats.Conn, error) {
	url := fmt.Sprintf("nats://%s:%d", creds.Host, creds.Port)

	opts := []nats.Option{nats.Name(name)}
	if creds.User != "" {
		opts = append(opts, nats.UserInfo(creds.User, creds.Password))
	}

	deadline := time.Now().Add(natsConnectTimeout)
	var lastErr error
	for {
		nc, err := nats.Connect(url, opts...)
		if err == nil {
			log.Info().Str("url", url).Msg("connected to NATS")
			return nc, nil
		}
		lastErr = err
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("connect %s after %s: %w", url, natsConnectTimeout, lastErr)
		}
		time.Sleep(natsConnectRetry)
	}
}
