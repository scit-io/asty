package process

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// tailReadBuffer is the per-tick read size. 4 KiB is one filesystem
// block on every supported OS, which keeps the syscall count down
// without holding too much in memory.
const tailReadBuffer = 4096

// GetLogPath returns the path to the active per-service log file.
// Rotated copies (when rotation is enabled) live at path.1, path.2, ...
func (p *Process) GetLogPath() string {
	return filepath.Join(p.workDir, "logs", fmt.Sprintf("%s.log", p.svc.Name))
}

// setupLogs prepares the destination the child process will write
// stdout/stderr into. Two modes:
//
//   - Plain (default, used when .asty leaves logs.* empty): child
//     writes directly to a single append-only file at GetLogPath().
//   - Rotating (logs.max_files > 0 && logs.max_file_size > 0): child
//     writes to a pipe; a copier goroutine drains the pipe into a
//     rotatingWriter that rolls the file over once max_file_size is
//     reached, keeping max_files rotated backups.
func (p *Process) setupLogs() error {
	logDir := filepath.Join(p.workDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logPath := p.GetLogPath()

	if p.svc.Logs.MaxFiles > 0 && p.svc.Logs.MaxFileSize > 0 {
		maxBytes := int64(p.svc.Logs.MaxFileSize) * 1024 * 1024
		rw, err := newRotatingWriter(logPath, maxBytes, p.svc.Logs.MaxFiles)
		if err != nil {
			return err
		}
		r, w, err := os.Pipe()
		if err != nil {
			rw.Close()
			return err
		}
		p.logRotator = rw
		p.logPipeW = w
		p.logWG.Add(1)
		go func() {
			defer p.logWG.Done()
			io.Copy(rw, r)
			r.Close()
		}()
		return nil
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	p.logFile = f
	return nil
}

// closeLogs releases whatever logs.go opened in setupLogs. In rotating
// mode this closes the pipe write end (which lets the copier goroutine
// drain and exit), waits for it, then closes the rotator. In plain
// mode it just closes the active file.
func (p *Process) closeLogs() {
	if p.logPipeW != nil {
		p.logPipeW.Close()
		p.logPipeW = nil
		p.logWG.Wait()
		if p.logRotator != nil {
			p.logRotator.Close()
			p.logRotator = nil
		}
		return
	}
	if p.logFile != nil {
		p.logFile.Close()
		p.logFile = nil
	}
}

// GetLogs returns the last `lines` lines of the active log file by
// scanning backwards from EOF in tailReadBuffer-sized chunks and
// counting newlines. Memory use is bounded by the returned tail, not
// by the file size. lines <= 0 returns the whole file. Rotated backups
// (path.1, .2, ...) are not included — callers wanting historical
// content read those files directly.
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
