package xws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// Manager is the registry of active WS sessions.
// Thread-safe: several goroutines (NATS callbacks, timers) may open,
// close, and read sessions concurrently.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*session
	nc       *nats.Conn
	timeout  time.Duration
	log      zerolog.Logger
}

// NewManager builds a Manager with the given inactivity timeout.
func NewManager(nc *nats.Conn, timeout time.Duration, log zerolog.Logger) *Manager {
	return &Manager{
		sessions: make(map[string]*session),
		nc:       nc,
		timeout:  timeout,
		log:      log,
	}
}

// Open registers a new session for SID received in the gateway connect
// message, subscribes to the inbound stream, and starts the inactivity
// timer.
//
// If a session with the same SID already exists, the call is ignored —
// this guards against re-delivery of the connect message after a NATS
// reconnect.
func (m *Manager) Open(sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sid]; exists {
		m.log.Warn().Str("sid", sid).Msg("session already exists, skipping")
		return
	}

	inSubj := "api.v1.xws.ws.in." + sid
	outSubj := "api.v1.xws.ws.out." + sid

	sess := &session{
		sid:     sid,
		outSubj: outSubj,
		nc:      m.nc,
		timeout: m.timeout,
		log:     m.log,
	}

	// Inactivity timer: on expiry, publish CLOSE and remove the session.
	sess.timer = time.AfterFunc(m.timeout, func() {
		m.log.Info().Str("sid", sid).Dur("timeout", m.timeout).Msg("inactivity timeout")
		sess.close()
		m.remove(sid)
	})

	// Each session uses a unique subject — no Queue Group needed:
	// the SID must be handled by the instance that opened it.
	sub, err := m.nc.Subscribe(inSubj, func(msg *nats.Msg) {
		m.handleIncoming(sess, msg)
	})
	if err != nil {
		sess.timer.Stop()
		m.log.Error().Err(err).Str("sid", sid).Msg("subscribe error")
		return
	}
	sess.inSub = sub
	m.sessions[sid] = sess

	// Acknowledge the connection to the client.
	sess.send(OutMsg{
		Type: "connected",
		SID:  sid,
		Text: "Connection will close after " + m.timeout.String() + " of inactivity.",
	})

	m.log.Info().Str("sid", sid).Dur("timeout", m.timeout).Msg("session opened")
}

// handleIncoming processes a message received from the browser. Any
// message resets the inactivity timer.
func (m *Manager) handleIncoming(sess *session, msg *nats.Msg) {
	// Any inbound message indicates the client is still alive.
	sess.resetTimer()

	var in InMsg
	if err := json.Unmarshal(msg.Data, &in); err != nil {
		m.log.Warn().Err(err).Str("sid", sess.sid).Msg("invalid JSON")
		return
	}

	switch in.Type {
	case "ping":
		// Client heartbeat — confirm the connection is alive.
		sess.send(OutMsg{Type: "pong"})

	case "message":
		// Echo response. Business logic of a real service goes here.
		sess.send(OutMsg{Type: "message", Text: "echo: " + in.Text})

	case "disconnect":
		// Client explicitly asked to close — don't wait for the timeout.
		m.log.Info().Str("sid", sess.sid).Msg("client requested disconnect")
		sess.timer.Stop()
		sess.close()
		m.remove(sess.sid)

	default:
		m.log.Warn().Str("sid", sess.sid).Str("type", in.Type).Msg("unknown message type")
	}
}

// remove drops the session from the registry.
func (m *Manager) remove(sid string) {
	m.mu.Lock()
	delete(m.sessions, sid)
	m.mu.Unlock()
}

// CloseAll terminates every active session. Called on service shutdown
// (SIGTERM) — browsers receive CLOSE and don't hang with an open
// connection.
//
// Snapshot under the lock, close outside it: session.close() does
// nc.PublishMsg + Unsubscribe; holding Manager.mu across network I/O
// would block Open/remove for the entire shutdown window if NATS were
// slow (up to the agent's kill_timeout before SIGKILL).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	snap := make([]*session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		snap = append(snap, sess)
	}
	m.sessions = make(map[string]*session)
	m.mu.Unlock()

	// session.close() is idempotent (sess.mu + closed flag); a parallel
	// remove(sid) for an already-absent sid is a no-op (delete on a map).
	for _, sess := range snap {
		sess.timer.Stop()
		sess.close()
	}
	m.log.Info().Msg("all sessions closed")
}
