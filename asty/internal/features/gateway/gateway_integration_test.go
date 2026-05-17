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

	"runtime/debug"

	"asty/asty/internal/core/config"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
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
	gw, err := New(ctx, conn, cfg, "test-node", nil, log)
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
		testRecover(log, func(msg *nats.Msg) { panic("integration boom") }),
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

// testRecover wraps a NATS handler with panic recovery — test-only
// equivalent of the production middleware used by backend services.
func testRecover(log zerolog.Logger, next nats.MsgHandler) nats.MsgHandler {
	return func(msg *nats.Msg) {
		defer func() {
			if r := recover(); r != nil {
				event := log.Error().
					Str("subject", msg.Subject).
					Interface("panic", r).
					Str("stack", string(debug.Stack()))
				if reqID := msg.Header.Get("X-Request-Id"); reqID != "" {
					event = event.Str("req", reqID)
				}
				event.Msg("panic in handler")

				if msg.Reply == "" {
					return
				}
				out := nats.NewMsg(msg.Reply)
				out.Header.Set("Content-Type", "application/json")
				out.Header.Set("Status", "500")
				out.Data = []byte(`{"error":"internal server error"}`)
				_ = msg.RespondMsg(out)
			}
		}()
		next(msg)
	}
}
