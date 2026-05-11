package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"asty/internal/platform/asty/features/gateway/metrics"

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

	// Metrics are incremented once per request — after the final status
	// is set. If status stayed at 0, the handler did not get to
	// respond (panic, client closed early) — those are excluded so
	// they do not skew error rate.
	//
	// metricUnknown=true collapses labels to "unknown" — caps a
	// cardinality bomb via /v1/{rand}/{rand}. Set after ErrNoResponders
	// outlives the retry window: the subject either doesn't exist or
	// has no subscribers across the full timeout.
	metricStatus := 0
	metricUnknown := false
	defer func() {
		if metricStatus == 0 {
			return
		}
		s, m := service, method
		if metricUnknown {
			s, m = "unknown", "unknown"
		}
		metrics.HTTPRequestsTotal.WithLabelValues(s, m, strconv.Itoa(metricStatus)).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(s, m).Observe(time.Since(start).Seconds())
	}()

	gw.log.Info().Str("req", reqID).Str("method", r.Method).Str("path", r.URL.Path).Str("ip", ip).Msg("→")

	body, ok := gw.readBody(w, r, reqID, subject, &metricStatus)
	if !ok {
		return
	}

	msg := buildRequestMsg(subject, body, reqID, ip, r)

	ctx, cancel := context.WithTimeout(r.Context(), gw.cfg.HTTP.NATSRequestTimeout)
	defer cancel()

	resp, attempts, dur, err := gw.natsRoundTrip(ctx, msg)
	if errors.Is(err, nats.ErrNoResponders) {
		metricUnknown = true
	}
	metricService := service
	if metricUnknown {
		metricService = "unknown"
	}
	metrics.NATSRequestDuration.WithLabelValues(metricService).Observe(dur.Seconds())
	metrics.NATSRequestAttemptsTotal.WithLabelValues(metricService, natsRequestOutcome(r.Context(), err)).Add(float64(attempts))
	if err != nil {
		status, reason := natsRequestErrStatus(r.Context(), err)
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Int("attempts", attempts).Int("status", status).Str("reason", reason).Msg("NATS request error")
		http.Error(w, reason, status)
		metricStatus = status
		return
	}

	statusCode := writeResponse(w, resp, reqID)
	metricStatus = statusCode

	if _, err := w.Write(resp.Data); err != nil { //nolint:gosec
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Msg("response write error")
		return
	}

	gw.log.Info().Str("req", reqID).Str("subject", subject).Int("status", statusCode).Int("attempts", attempts).Int64("ms", time.Since(start).Milliseconds()).Msg("←")
}

// readBody enforces a 1 MB cap and reports back to the caller via
// metricStatus on error. Returns (body, false) when it has already
// written the error response.
func (gw *Gateway) readBody(w http.ResponseWriter, r *http.Request, reqID, subject string, metricStatus *int) ([]byte, bool) {
	const maxBodySize = 1 << 20 // 1 MB
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
	if err != nil {
		gw.log.Error().Err(err).Str("req", reqID).Str("subject", subject).Msg("read body error")
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		*metricStatus = http.StatusBadRequest
		return nil, false
	}
	return body, true
}

// buildRequestMsg builds the NATS message with the standard set of
// headers gateway sends downstream.
func buildRequestMsg(subject string, body []byte, reqID, ip string, r *http.Request) *nats.Msg {
	msg := nats.NewMsg(subject)
	msg.Data = body
	msg.Header.Set("X-Real-IP", ip)
	msg.Header.Set("X-Request-Id", reqID)

	if auth := r.Header.Get("Authorization"); auth != "" {
		msg.Header.Set("Authorization", auth)
	}
	// HTTP/1.1 through a proxy/middlebox may arrive with several Cookie
	// headers (RFC 6265 §5.4 doesn't forbid it). r.Header.Get returns
	// only the first — backend would lose part of the cookies. Values +
	// Join with "; " collects everything into a single string.
	if cookies := strings.Join(r.Header.Values("Cookie"), "; "); cookies != "" {
		msg.Header.Set("Cookie", cookies)
	}
	return msg
}

// natsRoundTrip issues the request with retries on ErrNoResponders.
// Retries continue until ctx is done; non-NoResponders errors are
// terminal. Returns the response, the count of attempts (always ≥ 1),
// the elapsed time, and the final error.
func (gw *Gateway) natsRoundTrip(ctx context.Context, msg *nats.Msg) (*nats.Msg, int, time.Duration, error) {
	natsStart := time.Now()
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
			return resp, attempts, time.Since(natsStart), err
		case <-time.After(gw.cfg.HTTP.NATSRetryDelay):
		}
	}
	return resp, attempts, time.Since(natsStart), err
}

// writeResponse copies headers from the NATS reply to the HTTP
// response, picks the status code, and returns it.
func writeResponse(w http.ResponseWriter, resp *nats.Msg, reqID string) int {
	statusCode := http.StatusOK
	for k, values := range resp.Header {
		switch k {
		case "Status":
			if len(values) > 0 {
				if n := parseStatus(values[0]); n != 0 {
					statusCode = n
				}
			}
		case "Set-Cookie":
			for _, v := range values {
				w.Header().Add("Set-Cookie", v)
			}
		default:
			if len(values) > 0 {
				w.Header().Set(k, values[0])
			}
		}
	}
	w.Header().Set("X-Request-Id", reqID)
	w.WriteHeader(statusCode)
	return statusCode
}
