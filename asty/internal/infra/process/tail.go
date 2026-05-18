package process

import (
	"context"
	"io"
	"os"
	"syscall"
	"time"
)

// tailPollInterval — how often TailLogs re-reads the active log file.
// Fast enough to feel "live" in the UI; slow enough that an idle
// process doesn't burn a CPU.
const tailPollInterval = 100 * time.Millisecond

// TailLogs streams new lines from the active log file into the lines
// channel until ctx is cancelled. Seeks to EOF first so callers only
// see content written after the call started. When the active log is
// rotated out from underneath the reader (detected via inode change),
// the reader switches to the fresh file and continues from its start.
func (p *Process) TailLogs(ctx context.Context, lines chan<- string) error {
	f, err := os.Open(p.GetLogPath())
	if err != nil {
		return err
	}
	defer func() { f.Close() }()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	info, err := f.Stat()
	if err != nil {
		return err
	}
	inode := statInode(info)

	buf := make([]byte, tailReadBuffer)
	remainder := ""

	ticker := time.NewTicker(tailPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if pathInfo, statErr := os.Stat(p.GetLogPath()); statErr == nil {
				if pathIno := statInode(pathInfo); pathIno != 0 && pathIno != inode {
					f.Close()
					newF, openErr := os.Open(p.GetLogPath())
					if openErr != nil {
						return openErr
					}
					f = newF
					inode = pathIno
					remainder = ""
				}
			}

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

// statInode pulls the OS inode number out of a FileInfo. Used by
// TailLogs to detect log rotation. Returns 0 on platforms that don't
// expose a Stat_t — there, rotation just causes the tail to stop
// receiving data until the user re-subscribes.
func statInode(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}
