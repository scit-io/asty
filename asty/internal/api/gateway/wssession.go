package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsSession packages the synchronization primitives needed to coordinate
// the read loop, the Ping goroutine, the shutdown goroutine, and the
// NATS subscription callback that all touch the same conn.
type wsSession struct {
	conn   *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc

	// mu guards every call on conn (Write, Close, SetReadDeadline).
	// ctx.Done() under mu acts as a barrier so a writer never lands
	// on an already-closed session.
	mu sync.Mutex

	// wg awaits Ping + shutdown goroutines before final conn.Close.
	wg sync.WaitGroup
}

func newWSSession(conn *websocket.Conn, ctx context.Context, cancel context.CancelFunc) *wsSession {
	return &wsSession{conn: conn, ctx: ctx, cancel: cancel}
}

// write performs WriteMessage under mu, returning ctx.Err() if the
// session already shut down.
func (s *wsSession) write(msgType int, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}
	s.conn.SetWriteDeadline(time.Now().Add(wsWriteDeadline))
	return s.conn.WriteMessage(msgType, data)
}

// setReadDeadline unblocks ReadMessage from another goroutine.
func (s *wsSession) setReadDeadline(t time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}
	return s.conn.SetReadDeadline(t)
}

// close cancels, waits for helpers, and closes the connection. Order:
// cancel → wg.Wait → mu.Lock → conn.Close. wg.Wait drains Ping +
// shutdown goroutines so they don't touch conn after Close; mu.Lock
// drains any in-flight write from the NATS callback.
func (s *wsSession) close() {
	s.cancel()
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.conn.Close()
}
