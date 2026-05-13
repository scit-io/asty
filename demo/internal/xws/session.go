package xws

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// InMsg is an inbound message from the browser.
type InMsg struct {
	// Type is the message kind: "ping", "message", "disconnect".
	Type string `json:"type"`
	// Text is arbitrary payload (only used when type="message").
	Text string `json:"text,omitempty"`
}

// OutMsg is an outbound message sent to the browser.
type OutMsg struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	SID  string `json:"sid,omitempty"`
}

// session is an active WebSocket session for a single client.
//
// mu serialises close() / resetTimer() with the timer's AfterFunc
// callback: without it, Reset() could race with an already-firing
// callback (the Go docs forbid this), and the disconnect branch + timer
// could both call Unsubscribe + PublishMsg(CLOSE) twice.
type session struct {
	mu      sync.Mutex
	closed  bool
	sid     string
	outSubj string // api.v1.xws.ws.out.{sid}
	inSub   *nats.Subscription
	timer   *time.Timer // inactivity timer; reset on every received message
	nc      *nats.Conn
	timeout time.Duration
	log     zerolog.Logger
}

// send publishes OutMsg to the session's outbound NATS subject.
func (s *session) send(out OutMsg) {
	body, err := json.Marshal(out)
	if err != nil {
		s.log.Error().Err(err).Str("sid", s.sid).Msg("marshal error")
		return
	}
	if err := s.nc.Publish(s.outSubj, body); err != nil {
		s.log.Error().Err(err).Str("sid", s.sid).Msg("publish error")
	}
}

// close publishes a Control=CLOSE frame and unsubscribes from the
// inbound subject. The gateway receives the frame and closes the
// WebSocket connection from the server side. Idempotent: subsequent
// calls are no-ops.
func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.timer.Stop()
	s.mu.Unlock()

	msg := nats.NewMsg(s.outSubj)
	msg.Header.Set("Control", "CLOSE")
	if err := s.nc.PublishMsg(msg); err != nil {
		s.log.Error().Err(err).Str("sid", s.sid).Msg("CLOSE publish error")
	}
	if err := s.inSub.Unsubscribe(); err != nil {
		s.log.Error().Err(err).Str("sid", s.sid).Msg("unsubscribe error")
	}
	s.log.Info().Str("sid", s.sid).Msg("session closed")
}

// resetTimer restarts the inactivity timer on every incoming message.
// After close() it is a no-op, preventing a Reset-vs-AfterFunc race.
func (s *session) resetTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.timer.Reset(s.timeout)
}
