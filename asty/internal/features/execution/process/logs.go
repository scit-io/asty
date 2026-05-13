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

// GetLogs returns the last `lines` lines of the service log file by
// scanning backwards from EOF in tailReadBuffer-sized chunks and
// counting newlines. Memory use is bounded by the size of the returned
// tail, not by the log file size — important on long-running services
// that accumulate tens of MiB before anyone asks for logs. A non-
// positive `lines` reads the whole file.
func (p *Process) GetLogs(lines int) ([]byte, error) {
	f, err := os.Open(p.GetLogPath())
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if lines <= 0 {
		return io.ReadAll(f)
	}

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	buf := make([]byte, tailReadBuffer)
	var tail []byte
	newlines := 0
	pos := size

	for pos > 0 {
		readSize := int64(tailReadBuffer)
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		if _, err := f.ReadAt(buf[:readSize], pos); err != nil && err != io.EOF {
			return nil, err
		}
		chunk := buf[:readSize]
		for i := int(readSize) - 1; i >= 0; i-- {
			if chunk[i] != '\n' {
				continue
			}
			// Trailing newline at the very end of the file is a
			// separator, not a line boundary worth counting.
			if pos+int64(i) == size-1 {
				continue
			}
			newlines++
			if newlines == lines {
				return append(append([]byte{}, chunk[i+1:]...), tail...), nil
			}
		}
		tail = append(append([]byte{}, chunk...), tail...)
	}
	return tail, nil
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
