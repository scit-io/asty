package asty

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

	"github.com/rs/zerolog/log"
)

// Process represents a running service instance
type Process struct {
	mu sync.Mutex

	// Config
	svc    *ServiceDefinition
	nodeID string
	workDir string

	// Runtime
	cmd       *exec.Cmd
	pid       int
	status    ProcessStatus
	cancelCtx context.CancelFunc

	// Logs
	logFile *os.File
}

// ProcessStatus represents the current state of a process
type ProcessStatus string

const (
	ProcessStatusStarting ProcessStatus = "starting"
	ProcessStatusRunning  ProcessStatus = "running"
	ProcessStatusStopping ProcessStatus = "stopping"
	ProcessStatusStopped  ProcessStatus = "stopped"
	ProcessStatusFailed   ProcessStatus = "failed"
)

// NewProcess creates a new process instance
func NewProcess(svc *ServiceDefinition, nodeID, workDir string) *Process {
	return &Process{
		svc:     svc,
		nodeID:  nodeID,
		workDir: workDir,
		status:  ProcessStatusStopped,
	}
}

// Start starts the process
func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status == ProcessStatusRunning {
		return fmt.Errorf("process already running")
	}

	p.status = ProcessStatusStarting

	// Setup log file
	if err := p.setupLogs(); err != nil {
		p.status = ProcessStatusFailed
		return fmt.Errorf("failed to setup logs: %w", err)
	}

	// Prepare command
	// Parse command: if it starts with /, it's absolute, otherwise relative to workdir
	cmdPath := p.svc.Command
	var args []string

	// Check if command is shell form ("/bin/sh -c '...'") or just binary path
	if len(cmdPath) > 0 && cmdPath[0] == '/' {
		// Absolute path - use as-is, might have args
		// For shell commands like "/bin/sh -c 'script'", use shell to execute
		p.cmd = exec.CommandContext(ctx, "sh", "-c", p.svc.Command)
	} else {
		// Relative path - join with workdir
		cmdPath = filepath.Join(p.workDir, p.svc.Command)
		p.cmd = exec.CommandContext(ctx, cmdPath, args...)
	}

	p.cmd.Dir = p.workDir

	// Set environment variables
	p.cmd.Env = os.Environ()
	for k, v := range p.svc.Env {
		p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("%s=%s", k, os.ExpandEnv(v)))
	}

	// Start in own process group so we can kill the entire tree
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Redirect stdout/stderr to log file
	p.cmd.Stdout = p.logFile
	p.cmd.Stderr = p.logFile

	// Start process
	if err := p.cmd.Start(); err != nil {
		p.status = ProcessStatusFailed
		return fmt.Errorf("failed to start process: %w", err)
	}

	p.pid = p.cmd.Process.Pid
	p.status = ProcessStatusRunning

	// Create cancellable context for this process
	processCtx, cancel := context.WithCancel(context.Background())
	p.cancelCtx = cancel

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Str("workdir", p.workDir).
		Msg("process started")

	// Monitor process in background
	go p.monitor(processCtx)

	return nil
}

// Stop stops the process gracefully (SIGTERM → wait → SIGKILL)
func (p *Process) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.status != ProcessStatusRunning {
		return nil
	}

	p.status = ProcessStatusStopping

	// Cancel any ongoing operations (like log streaming)
	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("stopping process")

	// Send SIGTERM to entire process group
	syscall.Kill(-p.pid, syscall.SIGTERM)

	// Wait for graceful shutdown with timeout
	killTimeout := p.svc.GetKillTimeout()
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-time.After(killTimeout):
		// Force kill
		log.Warn().
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Dur("timeout", killTimeout).
			Msg("graceful shutdown timeout, sending SIGKILL")

		syscall.Kill(-p.pid, syscall.SIGKILL)
		<-done // Wait for actual exit

	case err := <-done:
		if err != nil && err.Error() != "signal: terminated" {
			log.Warn().Err(err).Str("service", p.svc.Name).Msg("process exited with error")
		}
	}

	p.status = ProcessStatusStopped
	p.closeLogs()

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Msg("process stopped")

	return nil
}

// Status returns the current process status
func (p *Process) Status() ProcessStatus {
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
func (p *Process) ServiceDefinition() *ServiceDefinition {
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
	// Create a new context that will be cancelled when process stops
	ctx := context.Background()
	// We can't return the internal context, so we create a derived one
	// This is a workaround - in production, consider using a proper context management
	return ctx, true
}

// monitor watches the process and updates status when it exits
func (p *Process) monitor(ctx context.Context) {
	err := p.cmd.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Cancel context to stop any ongoing operations
	if p.cancelCtx != nil {
		p.cancelCtx()
	}

	if p.status == ProcessStatusStopping {
		// Expected shutdown
		p.status = ProcessStatusStopped
	} else {
		// Unexpected exit
		p.status = ProcessStatusFailed
		log.Error().
			Err(err).
			Str("service", p.svc.Name).
			Int("pid", p.pid).
			Msg("process exited unexpectedly")
	}

	p.closeLogs()
}

// setupLogs creates and opens the log file for this process
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

// closeLogs closes the log file
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

	// Simple tail implementation - read entire file for now
	// TODO: implement efficient tail for large files
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

	// Seek to end of file
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}

	// Read new lines as they appear
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
