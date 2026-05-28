package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// handleHTTP proxies an HTTP request through NATS Request-Reply.
// Subject pattern: api.v1.{service}.{method.submethod...}
//
// Request → NATS headers: X-Real-IP, X-Request-Id, Authorization
// (if present), Cookie (merged from multiple Cookie headers).
//
// NATS → response headers: Status (controls HTTP status code; not
// copied as a header); Set-Cookie (each value Added so multiple
// cookies survive); everything else copied with Set.
func (gw *Gateway) handleHTTP(w http.ResponseWriter, r *http.Request, service string, methodParts []string) {
	reqID := newRequestID()
	method := strings.Join(methodParts, ".")
	subject := fmt.Sprintf("api.v1.%s.%s", service, method)
	ip := realIP(r, gw.cfg.RateLimit.TrustedProxy)
	start := time.Now()

	gw.log.Info().Str("req", reqID).Str("method", r.Method).Str("path", r.URL.Path).Str("ip", ip).Msg("→")

	body, ok := gw.readBody(w, r, reqID, subject)
	if !ok {
		return
	}

	msg := buildRequestMsg(subject, body, reqID, ip, r)

	ctx, cancel := context.WithTimeout(r.Context(), gw.cfg.HTTP.NATSRequestTimeout)
	defer cancel()

	resp, attempts, err := gw.natsRoundTrip(ctx, msg)
	if err != nil {
		status, reason := natsRequestErrStatus(r.Context(), err)
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Int("attempts", attempts).Int("status", status).Str("reason", reason).Msg("NATS request error")
		http.Error(w, reason, status)
		return
	}

	statusCode := writeResponse(w, resp, reqID)

	if _, err := w.Write(resp.Data); err != nil { //nolint:gosec
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Msg("response write error")
		return
	}

	gw.log.Info().Str("req", reqID).Str("subject", subject).Int("status", statusCode).Int("attempts", attempts).Int64("ms", time.Since(start).Milliseconds()).Msg("←")
}

// readBody enforces a 1 MB cap. Returns (body, false) when it has
// already written the error response.
func (gw *Gateway) readBody(w http.ResponseWriter, r *http.Request, reqID, subject string) ([]byte, bool) {
	const maxBodySize = 1 << 20 // 1 MB
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Msg("read body error")
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// buildRequestMsg forwards all HTTP headers into the NATS message.
// Gateway is a transparent proxy — it does not interpret or filter
// headers. Services decide what they need.
func buildRequestMsg(subject string, body []byte, reqID, ip string, r *http.Request) *nats.Msg {
	msg := nats.NewMsg(subject)
	msg.Data = body

	for key, values := range r.Header {
		for _, v := range values {
			msg.Header.Add(key, v)
		}
	}

	msg.Header.Set("X-Real-IP", ip)
	msg.Header.Set("X-Request-Id", reqID)
	return msg
}

// natsRoundTrip issues the request with retries on ErrNoResponders.
// Retries continue until ctx is done; non-NoResponders errors are
// terminal. Returns the response, the count of attempts (always ≥ 1),
// and the final error.
func (gw *Gateway) natsRoundTrip(ctx context.Context, msg *nats.Msg) (*nats.Msg, int, error) {
	attempts := 0
	var resp *nats.Msg
	var err error
	for {
		attempts++
		resp, err = gw.nats.RequestMsgWithContext(ctx, msg)
		if !errors.Is(err, nats.ErrNoResponders) {
			break
		}
		select {
		case <-ctx.Done():
			return resp, attempts, err
		case <-time.After(gw.cfg.HTTP.NATSRetryDelay):
		}
	}
	return resp, attempts, err
}

// writeResponse copies headers from the NATS reply to the HTTP
// response, picks the status code, and returns it.
//
// Status comes from ADR-32 service-error headers — the canonical NATS
// service-error contract. A reply with no Nats-Service-Error-Code is
// success → 200. Codes outside 100..599 collapse to 500 (valid per
// spec but not expressible as HTTP statuses). The two ADR-32 headers
// themselves are not propagated to the HTTP response; they're
// NATS-wire metadata, not HTTP semantics.
//
// Header copy uses Add so multi-value headers (e.g. multiple
// Set-Cookie on login) survive intact.
func writeResponse(w http.ResponseWriter, resp *nats.Msg, reqID string) int {
	statusCode := http.StatusOK
	if code, _ := readServiceError(resp.Header); code != 0 {
		if code >= 100 && code <= 599 {
			statusCode = code
		} else {
			statusCode = http.StatusInternalServerError
		}
	}
	for k, values := range resp.Header {
		if k == natsServiceErrorCode || k == natsServiceError {
			continue
		}
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Request-Id", reqID)
	w.WriteHeader(statusCode)
	return statusCode
}
