package asty

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
)

// ClusterLogger публикует важные cluster-level события в NATS для streaming через SSE
// Это не замена zerolog — это дополнительный канал для UI
type ClusterLogger struct {
	nc      *nats.Conn
	subject string
	mu      sync.Mutex
}

// NewClusterLogger создает новый cluster logger
func NewClusterLogger(nc *nats.Conn, nodeID string) *ClusterLogger {
	return &ClusterLogger{
		nc:      nc,
		subject: "asty.v1.server.logs",
	}
}

// LogEvent публикует событие в NATS
func (cl *ClusterLogger) LogEvent(level, message string, fields map[string]interface{}) {
	if cl.nc == nil {
		return
	}

	cl.mu.Lock()
	defer cl.mu.Unlock()

	// Создаем структурированный log entry
	entry := map[string]interface{}{
		"timestamp": time.Now().Unix(),
		"time":      time.Now().Format(time.RFC3339),
		"level":     level,
		"message":   message,
	}

	// Добавляем дополнительные поля
	for k, v := range fields {
		entry[k] = v
	}

	// Маршалим в JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	// Публикуем в NATS (fire-and-forget)
	cl.nc.Publish(cl.subject, data)
}

// Info логирует информационное событие
func (cl *ClusterLogger) Info(message string, fields map[string]interface{}) {
	cl.LogEvent("info", message, fields)
}

// Warn логирует предупреждение
func (cl *ClusterLogger) Warn(message string, fields map[string]interface{}) {
	cl.LogEvent("warn", message, fields)
}

// Error логирует ошибку
func (cl *ClusterLogger) Error(message string, fields map[string]interface{}) {
	cl.LogEvent("error", message, fields)
}

// GetSubject возвращает NATS subject для подписки
func (cl *ClusterLogger) GetSubject() string {
	return cl.subject
}

// NATSWriter is a zerolog writer that publishes to NATS
type NATSWriter struct {
	nc      *nats.Conn
	subject string
}

// NewNATSWriter creates a new NATS writer for zerolog
func NewNATSWriter(nc *nats.Conn, subject string) *NATSWriter {
	return &NATSWriter{
		nc:      nc,
		subject: subject,
	}
}

// Write implements io.Writer interface
func (w *NATSWriter) Write(p []byte) (n int, err error) {
	if w.nc == nil {
		return len(p), nil
	}

	// Parse zerolog JSON
	var entry map[string]interface{}
	if err := json.Unmarshal(p, &entry); err != nil {
		return len(p), nil // Skip malformed entries
	}

	// Add timestamp field if not present
	if _, ok := entry["timestamp"]; !ok {
		entry["timestamp"] = time.Now().Unix()
	}

	// Re-marshal and publish
	data, err := json.Marshal(entry)
	if err != nil {
		return len(p), nil
	}

	w.nc.Publish(w.subject, data)
	return len(p), nil
}

// LevelFilter wraps a writer and only writes logs at or above the given level
type LevelFilter struct {
	writer    zerolog.LevelWriter
	minLevel  zerolog.Level
	natsLevel zerolog.Level
}

// WriteLevel implements zerolog.LevelWriter
func (lf *LevelFilter) WriteLevel(level zerolog.Level, p []byte) (n int, err error) {
	// Always write to base writer if level is high enough
	if level >= lf.minLevel {
		return lf.writer.WriteLevel(level, p)
	}
	return len(p), nil
}

// Write implements io.Writer (fallback)
func (lf *LevelFilter) Write(p []byte) (n int, err error) {
	return lf.writer.Write(p)
}
