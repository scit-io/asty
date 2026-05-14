package gateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
)

// handleWS upgrades the HTTP connection to WebSocket and wires it
// into NATS Pub/Sub.
//
// Flow:
//   - Gateway → Service: connect-message with SID + cookies
//   - Browser → Gateway: frames are read and published to {base}.in.{sid}
//   - Service → Browser: messages on {base}.out.{sid} are written to WS
//   - Close: service sends Header "Control: CLOSE"
func (gw *Gateway) handleWS(parentCtx context.Context, w http.ResponseWriter, r *http.Request, service string) {
	ok, releaseConn := gw.wsConnGuard()
	if !ok {
		gw.log.Warn().Str("service", service).Int64("limit", gw.cfg.RateLimit.MaxWSConns).Msg("WS connections limit reached")
		http.Error(w, `{"error":"too many connections"}`, http.StatusServiceUnavailable)
		return
	}

	// Generate sessionID before Upgrade — if CSPRNG fails we can still
	// answer with plain HTTP 500. After Upgrade the connection is WS;
	// a graceful close would need a separate CloseMessage per RFC 6455.
	sidRaw := make([]byte, 8)
	if _, err := rand.Read(sidRaw); err != nil {
		releaseConn()
		gw.log.Error().Err(err).Str("service", service).Msg("session ID generation error")
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	sessionID := fmt.Sprintf("%x", sidRaw)

	conn, err := gw.upgrader.Upgrade(w, r, nil)
	if err != nil {
		releaseConn()
		gw.log.Error().Err(err).Str("service", service).Msg("WS upgrade error")
		return
	}
	defer releaseConn()
	conn.SetReadLimit(wsReadLimit)

	// Inherit from parentCtx (= gw.ctx): on server shutdown all WS
	// sessions cancel. r.Context() is intentionally not used — it
	// cancels when the HTTP handler returns; WS must live until
	// shutdown or explicit close.
	ctx, cancel := context.WithCancel(parentCtx)

	s := newWSSession(conn, ctx, cancel)
	defer s.close()

	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		return nil
	})

	baseSubject := fmt.Sprintf("api.v1.%s.ws", service)

	if err := gw.wsConnectAck(ctx, baseSubject, sessionID, r, s); err != nil {
		// wsConnectAck logs and writes CloseMessage on its own.
		return
	}

	sub, err := gw.wsSubscribeOut(baseSubject, sessionID, ctx, cancel, s)
	if err != nil {
		gw.log.Error().Err(err).Str("service", service).Str("sid", sessionID).Msg("WS subscribe error")
		return
	}
	defer sub.Unsubscribe()

	gw.wsHelperGoroutines(ctx, cancel, gw.ctx, s, sessionID)

	gw.log.Info().Str("service", service).Str("sid", sessionID).Str("remote", r.RemoteAddr).Msg("WS connected")
	defer gw.log.Info().Str("service", service).Str("sid", sessionID).Msg("WS disconnected")

	gw.wsReadLoop(ctx, conn, s, baseSubject, sessionID)
}

// wsConnectAck publishes the open-session control message to the target
// service and waits for an ack. On failure it writes a CloseMessage to
// the client so the browser does not hang until wsReadDeadline.
func (gw *Gateway) wsConnectAck(ctx context.Context, base, sid string, r *http.Request, s *wsSession) error {
	msg := nats.NewMsg(base + ".connect")
	msg.Header.Set("Sid", sid)
	if cookies := strings.Join(r.Header.Values("Cookie"), "; "); cookies != "" {
		msg.Header.Set("Cookie", cookies)
	}
	ctx, cancel := context.WithTimeout(ctx, gw.cfg.HTTP.WSConnectTimeout)
	defer cancel()

	if _, err := gw.nats.RequestMsgWithContext(ctx, msg); err != nil {
		event := gw.log.Error()
		if errors.Is(err, nats.ErrNoResponders) {
			event = gw.log.Warn()
		}
		event.Err(err).Str("sid", sid).Msg("WS connect ack error")
		_ = s.write(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "service unavailable"))
		return err
	}
	return nil
}

// wsSubscribeOut subscribes to the service-to-browser stream. The NATS
// callback writes to the WebSocket under the session's mutex.
func (gw *Gateway) wsSubscribeOut(base, sid string, ctx context.Context, cancel context.CancelFunc, s *wsSession) (*nats.Subscription, error) {
	return gw.nats.Subscribe(fmt.Sprintf("%s.out.%s", base, sid), func(m *nats.Msg) {
		// Fast exit without lock contention: a mass shutdown does not
		// pile dispatcher callbacks on s.mu — each sees ctx.Done early.
		if ctx.Err() != nil {
			return
		}
		if m.Header.Get("Control") == "CLOSE" {
			cancel()
			return
		}
		if err := s.write(websocket.TextMessage, m.Data); err != nil {
			if !errors.Is(err, context.Canceled) {
				gw.log.Error().Err(err).Str("sid", sid).Msg("WS write error")
			}
			cancel()
		}
	})
}

// wsHelperGoroutines starts the Ping and shutdown goroutines that
// outlive the read loop. Caller's defer s.close() waits for them.
func (gw *Gateway) wsHelperGoroutines(ctx context.Context, cancel context.CancelFunc, root context.Context, s *wsSession, sid string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(wsPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.write(websocket.PingMessage, nil); err != nil {
					gw.log.Error().Err(err).Str("sid", sid).Msg("WS ping error")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-root.Done():
			_ = s.write(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server restarting"))
			_ = s.setReadDeadline(time.Now())
		case <-ctx.Done():
		}
	}()
}

// wsReadLoop is the blocking browser → service direction.
func (gw *Gateway) wsReadLoop(ctx context.Context, conn *websocket.Conn, s *wsSession, base, sid string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
		_, data, err := conn.ReadMessage()
		if err != nil {
			switch {
			case websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway):
			case isTimeoutError(err):
				gw.log.Info().Str("sid", sid).Msg("WS session timed out")
				_ = s.write(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "inactivity timeout"))
			default:
				gw.log.Error().Err(err).Str("sid", sid).Msg("WS read error")
			}
			return
		}

		if err := gw.nats.Publish(fmt.Sprintf("%s.in.%s", base, sid), data); err != nil {
			gw.log.Error().Err(err).Str("sid", sid).Msg("NATS publish error")
		}
	}
}
