package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// Status represents the current state of a process.
type Status string

const (
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusStopped  Status = "stopped"
	StatusFailed   Status = "failed"
)

// Process represents a running service instance.
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

	// done is closed when the process exits (clean or failed). Letting
	// callers select on it avoids polling Status() in tight loops.
	done chan struct{}
	// onExit, if set, runs on the monitor goroutine after the process
	// exits. Used by the agent to drive the restart loop without a
	// scanning ticker.
	onExit func(err error)
}

// New creates a new process instance in the StatusStopped state.
func New(svc *types.ServiceDefinition, nodeID, workDir string) *Process {
	return &Process{
		svc:     svc,
		nodeID:  nodeID,
		workDir: workDir,
		status:  StatusStopped,
		done:    make(chan struct{}),
	}
}

// OnExit registers fn to run when the process exits. Must be called
// before Start. Replaces any previously-set callback.
func (p *Process) OnExit(fn func(err error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onExit = fn
}

// Done returns a channel that is closed once the process has exited
// (whether cleanly or as failure). Callers can select on it instead of
// polling Status(). Cheap to call repeatedly — the channel is created
// in New.
func (p *Process) Done() <-chan struct{} {
	return p.done
}

// Start launches the process. Absolute commands run via `sh -c` so shell
// expansions (env vars, pipes) work; relative commands are resolved
// against the per-service working directory.
//
// Each process gets its own process group (Setpgid) so Stop can signal
// the whole tree, not just the immediate child.
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

	monCtx, cancel := context.WithCancel(context.Background())
	p.cancelCtx = cancel

	log.Info().
		Str("service", p.svc.Name).
		Int("pid", p.pid).
		Str("workdir", p.workDir).
		Msg("process started")

	go p.monitor(monCtx)
	return nil
}

// Status returns the current process status.
func (p *Process) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// PID returns the process ID, or 0 if the process has not started.
func (p *Process) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// ServiceDefinition returns the service definition this process is
// running, useful for restart loops that need access to the kill_timeout
// or restart policy.
func (p *Process) ServiceDefinition() *types.ServiceDefinition {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.svc
}
