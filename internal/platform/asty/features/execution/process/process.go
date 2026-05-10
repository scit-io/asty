package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"asty/internal/platform/asty/core/types"

	"github.com/rs/zerolog/log"
)

// Status represents the current state of a process
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

// Process represents a running service instance
type Process struct {
	mu sync.Mutex

	svc     *types.ServiceDefinition
	nodeID  string
	workDir string

	cmd       *exec.Cmd
	pid       int
	status    Status
	cancelCtx context.CancelFunc

	logFile *os.File
}

// New creates a new process instance
func New(svc *types.ServiceDefinition, nodeID, workDir string) *Process {
	return &Process{
		svc:     svc,
		nodeID:  nodeID,
		workDir: workDir,
		status:  StatusStopped,
	}
}

// Start starts the process
func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == StatusRunning {
		return fmt.Errorf("process already running")
	}

	p.status = StatusStarting

	if err := p.setupLogs(); err != nil {
		p.status = StatusFailed
		return fmt.Errorf("failed to setup logs: %w", err)
	}

	cmdPath := p.svc.Command

	if len(cmdPath) > 0 && cmdPath[0] == '/' {
		p.cmd = exec.CommandContext(ctx, "sh", "-c", p.svc.Command)
	} else {
		cmdPath = filepath.Join(p.workDir, p.svc.Command)
		p.cmd = exec.CommandContext(ctx, cmdPath)
	}

	p.cmd.Dir = p.workDir

	p.cmd.Env = os.Environ()
	for k, v := range p.svc.Env {
		p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}

	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	p.cmd.Stdout = p.logFile
	p.cmd.Stderr = p.logFile

	if err := p.cmd.Start(); err != nil {
		p.status = StatusFailed
		return fmt.Errorf("failed to start process: %w", err)
	}

	p.pid = p.cmd.Process.Pid
	p.status = StatusRunning

	processCtx, cancel := context.WithCancel(context.Background())
	p.cancelCtx = cancel

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Str("workdir", p.workDir).
		Msg("process started")

	go p.monitor(processCtx)

	return nil
}

// Stop stops the process gracefully (SIGTERM → wait → SIGKILL)
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != StatusRunning {
		return nil
	}

	p.status = StatusStopping

	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("stopping process")

	syscall.Kill(-p.pid, syscall.SIGTERM)

	killTimeout := p.svc.GetKillTimeout()
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(killTimeout):
		log.Warn().
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Dur("timeout", killTimeout).
			Msg("graceful shutdown timeout, sending SIGKILL")

		syscall.Kill(-p.pid, syscall.SIGKILL)
		<-done

	case err := <-done:
		if err != nil && err.Error() != "signal: terminated" {
			log.Warn().Err(err).Str("service", p.svc.Name).Msg("process exited with error")
		}
	}

	p.status = StatusStopped
	p.closeLogs()

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("process stopped")

	return nil
}

// Status returns the current process status
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// PID returns the process ID
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// ServiceDefinition returns the service definition
func (p *Process) ServiceDefinition() *types.ServiceDefinition {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.svc
}

// Context returns a context that is cancelled when the process stops
func (p *Process) Context() (context.Context, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelCtx == nil {
		return nil, false
	}
	ctx := context.Background()
	return ctx, true
}

func (p *Process) monitor(ctx context.Context) {
	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	if p.status == StatusStopping {
		p.status = StatusStopped
	} else {
		p.status = StatusFailed
		log.Error().
			Err(err).
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Msg("process exited unexpectedly")
	}

	p.closeLogs()
}

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

// GetLogs returns the last N lines from the process log file
func (p *Process) GetLogs(lines int) ([]byte, error) {
	logPath := filepath.Join(p.workDir, "logs", fmt.Sprintf("%s.log", p.svc.Name))

	f, err := os.Open(logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// GetLogPath returns the path to the log file
func (p *Process) GetLogPath() string {
	return filepath.Join(p.workDir, "logs", fmt.Sprintf("%s.log", p.svc.Name))
}

// TailLogs streams new log lines as they are written
func (p *Process) TailLogs(ctx context.Context, lines chan<- string) error {
	logPath := p.GetLogPath()

	f, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	buf := make([]byte, 4096)
	remainder := ""

	ticker := time.NewTicker(100 * time.Millisecond)
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

			if n > 0 {
				data := remainder + string(buf[:n])
				linesData := ""

				for i := 0; i < len(data); i++ {
					if data[i] == '\n' {
						if linesData != "" {
							select {
							case lines <- linesData:
							case <-ctx.Done():
								return ctx.Err()
							}
							linesData = ""
						}
					} else {
						linesData += string(data[i])
					}
				}

				remainder = linesData
			}
		}
	}
}
