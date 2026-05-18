package process

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// rotatingWriter is an io.Writer that writes to path. When the active
// file size exceeds maxBytes, it is closed, renamed to path.1 (with
// older rotations shifted .1 → .2, ..., .{N-1} → .N and the oldest
// path.{N} dropped), and a fresh file is opened at path. Writes are
// serialised by mu; one rotation never splits a Write call.
//
// maxFiles counts rotated copies kept (not including the active file),
// so disk use peaks at roughly (maxFiles+1) × maxBytes.
type rotatingWriter struct {
	path     string
	maxBytes int64
	maxFiles int

	mu      sync.Mutex
	f       *os.File
	written int64
}

func newRotatingWriter(path string, maxBytes int64, maxFiles int) (*rotatingWriter, error) {
	rw := &rotatingWriter{
		path:     path,
		maxBytes: maxBytes,
		maxFiles: maxFiles,
	}
	if err := rw.openActive(); err != nil {
		return nil, err
	}
	return rw, nil
}

func (rw *rotatingWriter) openActive() error {
	f, err := os.OpenFile(rw.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	rw.f = f
	rw.written = info.Size()
	return nil
}

func (rw *rotatingWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	if rw.f == nil {
		return 0, io.ErrClosedPipe
	}
	// Rotate *before* writing if the incoming bytes would cross the
	// limit; rotating mid-write would split a log line across files.
	// The written > 0 guard prevents rotation of an empty file when a
	// single Write is itself larger than maxBytes — better to keep
	// such a giant entry intact in one file than to lose data.
	if rw.written+int64(len(p)) > rw.maxBytes && rw.written > 0 {
		if err := rw.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := rw.f.Write(p)
	rw.written += int64(n)
	return n, err
}

// rotateLocked shifts the backup chain down by one and renames the
// active file into the .1 slot. Per-rename errors are non-fatal: a
// missing intermediate just means there's nothing to shift at that
// index. Only the active-file rename is treated as fatal.
func (rw *rotatingWriter) rotateLocked() error {
	if err := rw.f.Close(); err != nil {
		return err
	}
	rw.f = nil

	os.Remove(fmt.Sprintf("%s.%d", rw.path, rw.maxFiles))
	for i := rw.maxFiles - 1; i >= 1; i-- {
		os.Rename(
			fmt.Sprintf("%s.%d", rw.path, i),
			fmt.Sprintf("%s.%d", rw.path, i+1),
		)
	}
	if err := os.Rename(rw.path, rw.path+".1"); err != nil {
		return err
	}
	return rw.openActive()
}

func (rw *rotatingWriter) Close() error {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	if rw.f == nil {
		return nil
	}
	err := rw.f.Close()
	rw.f = nil
	return err
}
