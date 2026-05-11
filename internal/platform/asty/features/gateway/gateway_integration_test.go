//go:build integration

package gateway

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"asty/internal/middleware"
	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/metrics"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
)

// safeBuf is a thread-safe writer used to capture logs from gateway/
// Recover goroutines.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// newTestGateway builds a Gateway pointed at conn with the supplied
// config. The caller must invoke the returned cancel func when done.
func newTestGateway(t *testing.T, conn *nats.Conn, cfg config.GatewayConfig, log zerolog.Logger) (*Gateway, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	gw, err := New(ctx, conn, cfg, log)
	if err != nil {
		cancel()
		t.Fatalf("gateway New: %v", err)
	}
	return gw, cancel
}

// defaultTestCfg is a small set of sane defaults; tests can override
// any field with a literal.
func defaultTestCfg() config.GatewayConfig {
	return config.GatewayConfig{
		Enabled: true,
		HTTP: config.GatewayHTTPConfig{
			NATSRequestTimeout: 2 * time.Second,
			NATSRetryDelay:     50 * time.Millisecond,
		},
		RateLimit: config.GatewayRateLimitConfig{
			Rate: 1000, Burst: 100, MaxIPs: 100, MaxWSConns: 100,
		},
	}
}

// TestIntegration_HTTPToNATSRoundTrip verifies the full path:
// HTTP client → Gateway → NATS Request → backend subscriber → NATS Reply
// → Gateway → HTTP response.
func TestIntegration_HTTPToNATSRoundTrip(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	sub, err := conn.Subscribe("api.v1.echo.ping", func(m *nats.Msg) {
		resp := &nats.Msg{Header: nats.Header{}, Data: []byte(`{"echo":"` + string(m.Data) + `"}`)}
		resp.Header.Set("X-Custom", "value")
		_ = m.RespondMsg(resp)
	})
	if err != nil {
		t.Fatalf("nats subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	if err := conn.Flush(); err != nil {
		t.Fatalf("nats flush: %v", err)
	}

	gw, cancel := newTestGateway(t, conn, defaultTestCfg(), zerolog.New(io.Discard))
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/v1/echo/ping", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"echo":"hello"}` {
		t.Errorf("body = %q, want %q", string(body), `{"echo":"hello"}`)
	}
	if got := resp.Header.Get("X-Custom"); got != "value" {
		t.Errorf("X-Custom header = %q, want value", got)
	}
	if got := resp.Header.Get("X-Request-Id"); got == "" {
		t.Errorf("X-Request-Id header missing")
	}
}

// TestIntegration_MetricsCounterIncrements checks that an HTTP request
// through the gateway increments gateway_http_requests_total and the
// metric is exposed on /metrics in Prometheus format.
func TestIntegration_MetricsCounterIncrements(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	sub, err := conn.Subscribe("api.v1.echo.ping", func(m *nats.Msg) {
		_ = m.Respond([]byte(`{}`))
	})
	if err != nil {
		t.Fatalf("nats subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	if err := conn.Flush(); err != nil {
		t.Fatalf("nats flush: %v", err)
	}

	gw, cancel := newTestGateway(t, conn, defaultTestCfg(), zerolog.New(io.Discard))
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	metricsSrv := httptest.NewServer(promhttp.Handler())
	defer metricsSrv.Close()

	baseline := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("echo", "ping", "200"))

	resp, err := http.Post(httpSrv.URL+"/v1/echo/ping", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	got := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("echo", "ping", "200"))
	if got-baseline != 1 {
		t.Errorf("counter delta = %v, want 1", got-baseline)
	}

	mResp, err := http.Get(metricsSrv.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics scrape: %v", err)
	}
	defer mResp.Body.Close()
	body, err := io.ReadAll(mResp.Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	bodyStr := string(body)
	expectedLines := []string{
		`gateway_http_requests_total{method="ping",service="echo",status="200"}`,
		`gateway_http_request_duration_seconds_count{method="ping",service="echo"}`,
		`nats_request_duration_seconds_count{service="echo"}`,
		`nats_request_attempts_total{outcome="ok",service="echo"}`,
	}
	for _, line := range expectedLines {
		if !strings.Contains(bodyStr, line) {
			t.Errorf("metrics endpoint missing line %q", line)
		}
	}
}

// TestIntegration_MetricsUnknownService guards the cardinality bomb:
// a request to a non-existent subject (/v1/{rand}/{rand}) gets
// ErrNoResponders after the retry window and is recorded under
// service="unknown",method="unknown".
func TestIntegration_MetricsUnknownService(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	cfg := defaultTestCfg()
	cfg.HTTP.NATSRequestTimeout = 200 * time.Millisecond
	gw, cancel := newTestGateway(t, conn, cfg, zerolog.New(io.Discard))
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	baselineUnknown := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("unknown", "unknown", "503"))
	baselineFoo := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("foo", "bar", "503"))
	baselineNATSUnknown := testutil.ToFloat64(metrics.NATSRequestAttemptsTotal.WithLabelValues("unknown", "no_responders"))
	baselineNATSFoo := testutil.ToFloat64(metrics.NATSRequestAttemptsTotal.WithLabelValues("foo", "no_responders"))

	resp, err := http.Post(httpSrv.URL+"/v1/foo/bar", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}

	gotUnknown := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("unknown", "unknown", "503"))
	if gotUnknown-baselineUnknown != 1 {
		t.Errorf("HTTPRequestsTotal{service=unknown,method=unknown,status=503} delta = %v, want 1", gotUnknown-baselineUnknown)
	}
	gotFoo := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("foo", "bar", "503"))
	if gotFoo-baselineFoo != 0 {
		t.Errorf("HTTPRequestsTotal{service=foo,method=bar,status=503} delta = %v, want 0 (cardinality bomb)", gotFoo-baselineFoo)
	}
	gotNATSUnknown := testutil.ToFloat64(metrics.NATSRequestAttemptsTotal.WithLabelValues("unknown", "no_responders"))
	if gotNATSUnknown-baselineNATSUnknown < 1 {
		t.Errorf("NATSRequestAttemptsTotal{service=unknown,outcome=no_responders} delta = %v, want >= 1", gotNATSUnknown-baselineNATSUnknown)
	}
	gotNATSFoo := testutil.ToFloat64(metrics.NATSRequestAttemptsTotal.WithLabelValues("foo", "no_responders"))
	if gotNATSFoo-baselineNATSFoo != 0 {
		t.Errorf("NATSRequestAttemptsTotal{service=foo,outcome=no_responders} delta = %v, want 0 (cardinality bomb)", gotNATSFoo-baselineNATSFoo)
	}
}

// TestIntegration_MetricsRateLimitRejected verifies that the
// gateway_rate_limit_rejected_total counter advances when the limit
// fires.
func TestIntegration_MetricsRateLimitRejected(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	cfg := defaultTestCfg()
	cfg.HTTP.NATSRequestTimeout = time.Second
	cfg.RateLimit = config.GatewayRateLimitConfig{Rate: 0.1, Burst: 1, MaxIPs: 10, MaxWSConns: 10}
	gw, cancel := newTestGateway(t, conn, cfg, zerolog.New(io.Discard))
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	baseline := testutil.ToFloat64(metrics.RateLimitRejectedTotal.WithLabelValues("general"))

	for range 2 {
		resp, _ := http.Get(httpSrv.URL + "/v1/echo/ping")
		if resp != nil {
			resp.Body.Close()
		}
	}

	got := testutil.ToFloat64(metrics.RateLimitRejectedTotal.WithLabelValues("general"))
	if got-baseline < 1 {
		t.Errorf("rate_limit_rejected delta = %v, want >= 1", got-baseline)
	}
}

// TestIntegration_PanicPropagatesRequestID drives a request that
// panics in the backend through Gateway + middleware.Recover and
// confirms X-Request-Id flows end-to-end and into both log streams.
func TestIntegration_PanicPropagatesRequestID(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	var logBuf safeBuf
	log := zerolog.New(&logBuf)

	sub, err := conn.QueueSubscribe(
		"api.v1.boom.panic",
		"boom",
		middleware.Recover(log, func(msg *nats.Msg) { panic("integration boom") }),
	)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	gw, cancel := newTestGateway(t, conn, defaultTestCfg(), log)
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	resp, err := http.Post(httpSrv.URL+"/v1/boom/panic", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != `{"error":"internal server error"}` {
		t.Errorf("body = %q, want internal server error JSON", string(body))
	}

	reqID := resp.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Fatalf("X-Request-Id header missing in 500 response")
	}

	logStr := logBuf.String()
	pattern := regexp.MustCompile(`"req":"` + regexp.QuoteMeta(reqID) + `"`)
	if !pattern.MatchString(logStr) {
		t.Errorf("log missing request ID %q, got: %s", reqID, logStr)
	}
	if !strings.Contains(logStr, `"panic":"integration boom"`) {
		t.Errorf("log missing panic value, got: %s", logStr)
	}
}

// TestIntegration_HealthEndpoint exercises the /health endpoint:
// 200 while NATS is connected, 503 once the connection is closed.
func TestIntegration_HealthEndpoint(t *testing.T) {
	srv := natsserver.RunRandClientPortServer()
	defer srv.Shutdown()

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer conn.Close()

	cfg := defaultTestCfg()
	cfg.HTTP.NATSRequestTimeout = time.Second
	cfg.RateLimit = config.GatewayRateLimitConfig{Rate: 100, Burst: 10, MaxIPs: 100, MaxWSConns: 100}
	gw, cancel := newTestGateway(t, conn, cfg, zerolog.New(io.Discard))
	defer cancel()

	httpSrv := httptest.NewServer(gw.Handler())
	defer httpSrv.Close()

	resp, err := http.Get(httpSrv.URL + "/health")
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("connected health status = %d, want 200", resp.StatusCode)
	}

	conn.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && conn.IsConnected() {
		time.Sleep(20 * time.Millisecond)
	}
	if conn.IsConnected() {
		t.Fatalf("nats still reports connected after Close()")
	}

	resp2, err := http.Get(httpSrv.URL + "/health")
	if err != nil {
		t.Fatalf("disconnected health request: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("disconnected health status = %d, want 503", resp2.StatusCode)
	}
	body, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("read disconnected body: %v", err)
	}
	if string(body) != `{"status":"error","nats":"disconnected"}` {
		t.Errorf("disconnected body = %q, want %q", string(body), `{"status":"error","nats":"disconnected"}`)
	}
}
