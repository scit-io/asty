package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
)

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

	resp, err := gw.nats.RequestMsgWithContext(ctx, msg)
	if err != nil {
		event := gw.log.Error()
		if errors.Is(err, nats.ErrNoResponders) {
			event = gw.log.Warn()
		}
		event.Err(err).Str("sid", sid).Msg("WS connect ack error")
		_ = s.write(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "service unavailable"))
		return err
	}

	// ADR-32 client-side check: a service signals application-level
	// rejection (auth, validation, ...) via Nats-Service-Error-Code +
	// Nats-Service-Error headers. nats.go's auto ErrNoResponders only
	// covers the transport "no subscriber" case, so an unchecked WS
	// would open even when the service explicitly refused the upgrade.
	if code, desc := readServiceError(resp.Header); code != 0 {
		reason := desc
		if reason == "" {
			reason = fmt.Sprintf("rejected: %d", code)
		}
		if len(reason) > 120 { // RFC 6455 §5.5.1 — close-reason ≤ 123 bytes
			reason = reason[:120]
		}
		closeCode := websocket.ClosePolicyViolation
		if code >= 500 && code < 600 {
			closeCode = websocket.CloseInternalServerErr
		}
		gw.log.Warn().Str("sid", sid).Int("code", code).Str("reason", reason).Msg("WS connect rejected by service")
		_ = s.write(websocket.CloseMessage,
			websocket.FormatCloseMessage(closeCode, reason))
		return fmt.Errorf("ws connect rejected: %d", code)
	}
	return nil
}
