package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// tailPollInterval — how often TailLogs re-reads the log file. Fast
// enough to feel "live" in the UI; slow enough that an idle process
// doesn't burn a CPU. fsnotify would be more reactive but complicates
// log rotation; the polling cost is negligible compared to JSON encoding
// and NATS publish on the agent.
const tailPollInterval = 100 * time.Millisecond

// tailReadBuffer is the per-tick read size. 4 KiB is one filesystem
// block on every supported OS, which keeps the syscall count down
// without holding too much in memory.
const tailReadBuffer = 4096

func (p *Process) setupLogs() error {
	logDir := filepath.Join(p.workDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", p.svc.Name))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	p.logFile = f
	return nil
}

func (p *Process) closeLogs() {
	if p.logFile != nil {
		p.logFile.Close()
		p.logFile = nil
	}
}

// GetLogPath returns the path to the per-service log file.
func (p *Process) GetLogPath() string {
	return filepath.Join(p.workDir, "logs", fmt.Sprintf("%s.log", p.svc.Name))
}

// GetLogs reads the entire log file and returns it. The lines parameter
// is currently ignored; callers that want a tail use tailLines on the
// returned bytes.
//
// TODO: replace with a true tail-N implementation when log files grow
// large enough that loading the whole file becomes a memory concern.
func (p *Process) GetLogs(lines int) ([]byte, error) {
	f, err := os.Open(p.GetLogPath())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// TailLogs streams new lines from the log file into the lines channel
// until ctx is cancelled. It seeks to EOF first so callers only see
// content written after the call started.
func (p *Process) TailLogs(ctx context.Context, lines chan<- string) error {
	f, err := os.Open(p.GetLogPath())
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	buf := make([]byte, tailReadBuffer)
	remainder := ""

	ticker := time.NewTicker(tailPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			n, err := f.Read(buf)
			if err != nil && err != io.EOF {
				return err
			}
			if n == 0 {
				continue
			}
			remainder = emitLines(ctx, lines, remainder+string(buf[:n]))
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
}

// emitLines pushes complete lines from data into out, returning whatever
// trailing partial line is left for the next read cycle to prepend.
func emitLines(ctx context.Context, out chan<- string, data string) string {
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		line := data[start:i]
		start = i + 1
		if line == "" {
			continue
		}
		select {
		case out <- line:
		case <-ctx.Done():
			return ""
		}
	}
	return data[start:]
}
