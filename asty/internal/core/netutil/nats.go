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

// errText returns err.Error() or "" — keeps log fields clean when the
// closed-handler fires after a graceful Close (LastError is nil then).
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ConnectNATS opens a NATS connection using the supplied credentials and
// tags it with name (used in NATS server logs to tell agent/server apart).
// It retries on connection failure until natsConnectTimeout elapses so a
// peer process (agent supervises NATS) can finish bootstrapping the
// broker without taking the caller down.
//
// extraOpts are appended verbatim to the standard option set, letting
// callers attach DiscoveredServersHandler / ReconnectHandler / etc.
// without forking ConnectNATS.
//
// Default handlers are attached for slow-consumer / disconnect /
// reconnect / closed events. Without them nats.go drops these to
// stderr silently — observed: a 12→1 degrade produced 19 advisory
// publishes/30s confirmed via external probe, while the agent's
// SubscribeSync received zero, with no error in the agent log because
// the slow-consumer event went to the default discard sink.
//
// The caller owns the returned connection and must Close it when done.
func ConnectNATS(creds NATSCreds, name string, extraOpts ...nats.Option) (*nats.Conn, error) {
	url := fmt.Sprintf("nats://%s:%d", creds.Host, creds.Port)

	opts := []nats.Option{
		nats.Name(name),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			subj := ""
			if sub != nil {
				subj = sub.Subject
			}
			log.Warn().Str("conn", name).Str("subject", subj).Err(err).Msg("nats async error (slow consumer / permission / protocol)")
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn().Str("conn", name).Err(err).Msg("nats disconnected")
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			log.Info().Str("conn", name).Str("url", c.ConnectedUrl()).Msg("nats reconnected")
		}),
		nats.ClosedHandler(func(c *nats.Conn) {
			log.Warn().Str("conn", name).Str("last_err_text", errText(c.LastError())).Msg("nats closed")
		}),
	}
	if creds.User != "" {
		opts = append(opts, nats.UserInfo(creds.User, creds.Password))
	}
	opts = append(opts, extraOpts...)

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
