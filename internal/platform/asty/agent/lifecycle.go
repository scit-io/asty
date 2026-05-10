package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall"
	"time"

	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/execution/process"

	"github.com/rs/zerolog/log"
)

func (a *Agent) publishHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nodeInfo := a.getNodeInfo()

			if err := a.clusterState.UpdateNode(nodeInfo); err != nil {
				log.Error().Err(err).Msg("failed to update node heartbeat")
			} else {
				log.Debug().Str("node_id", a.nodeID).Msg("heartbeat sent")
			}
		}
	}
}

func (a *Agent) publishProcessMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.mu.RLock()
			procs := make(map[string]*process.Process, len(a.processes))
			for name, proc := range a.processes {
				procs[name] = proc
			}
			a.mu.RUnlock()

			for serviceName, proc := range procs {
				m, ok := a.metricsCollector.GetMetrics(proc.PID())
				if !ok {
					continue
				}
				cpu := int(m.CPUPercent)
				mem := int(m.MemoryMB)
				healthStatus := a.healthChecker.HealthStatusStr(serviceName)
				err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
					alloc.CPUUsage = cpu
					alloc.MemoryUsage = mem
					if healthStatus != "" {
						alloc.HealthStatus = healthStatus
					}
					return true
				})
				if err != nil {
					log.Error().Err(err).Str("service", serviceName).Msg("failed to update allocation metrics")
				}
			}
		}
	}
}

func (a *Agent) monitorProcesses(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.checkAndRestartFailedProcesses()
		}
	}
}

func (a *Agent) checkAndRestartFailedProcesses() {
	a.mu.Lock()
	failedProcesses := make(map[string]*process.Process)
	for name, proc := range a.processes {
		if proc.Status() == process.StatusFailed {
			failedProcesses[name] = proc
		}
	}
	a.mu.Unlock()

	for serviceName, proc := range failedProcesses {
		log.Warn().
			Str("service", serviceName).
			Int("pid", proc.PID()).
			Msg("detected failed process, attempting restart")

		svc := proc.ServiceDefinition()
		maxAttempts := svc.Restart.GetAttempts()

		var (
			giveUp      bool
			restarts    int
			consecutive int
		)
		err := a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
			if alloc.ConsecutiveFailures >= maxAttempts {
				alloc.Status = "failed"
				giveUp = true
			} else {
				alloc.Restarts++
				alloc.ConsecutiveFailures++
				restarts = alloc.Restarts
				consecutive = alloc.ConsecutiveFailures
			}
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to mutate allocation on failure")
			continue
		}

		if giveUp {
			log.Error().
				Str("service", serviceName).
				Int("max_attempts", maxAttempts).
				Msg("restart attempts exhausted, giving up")
			a.mu.Lock()
			delete(a.processes, serviceName)
			a.mu.Unlock()
			continue
		}

		if pid := proc.PID(); pid > 0 {
			syscall.Kill(-pid, syscall.SIGKILL)
			syscall.Kill(pid, syscall.SIGKILL)
		}
		a.mu.Lock()
		delete(a.processes, serviceName)
		a.mu.Unlock()
		a.healthChecker.Unregister(serviceName)
		a.metricsCollector.Unregister(proc.PID())

		log.Warn().
			Str("service", serviceName).
			Int("restarts", restarts).
			Int("consecutive_failures", consecutive).
			Int("old_pid", proc.PID()).
			Msg("restarting failed service")

		time.Sleep(svc.Restart.GetDelay())

		err = a.clusterState.MutateAllocation(serviceName, a.nodeID, func(alloc *types.ServiceAllocation) bool {
			alloc.Status = "pending"
			alloc.PID = 0
			return true
		})
		if err != nil {
			log.Error().Err(err).Str("service", serviceName).Msg("failed to mark allocation pending")
			continue
		}

		log.Info().
			Str("service", serviceName).
			Int("attempt", restarts).
			Msg("marked allocation for restart, waiting for server command")
	}
}

func (a *Agent) streamProcessLogs(serviceName string, proc *process.Process) {
	subject := fmt.Sprintf("asty.v1.agent.%s.logs.%s", a.nodeID, serviceName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logLines := make(chan string, 100)

	go func() {
		if err := proc.TailLogs(ctx, logLines); err != nil && err != context.Canceled {
			log.Error().
				Err(err).
				Str("service", serviceName).
				Msg("failed to tail logs")
		}
		close(logLines)
	}()

	log.Info().
		Str("service", serviceName).
		Str("subject", subject).
		Msg("streaming logs to NATS")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-logLines:
			if !ok {
				log.Info().
					Str("service", serviceName).
					Msg("log channel closed, ending stream")
				return
			}

			logEntry, err := json.Marshal(map[string]interface{}{
				"line":      line,
				"timestamp": time.Now().Unix(),
			})
			if err != nil {
				continue
			}

			if err := a.nc.Publish(subject, logEntry); err != nil {
				log.Error().
					Err(err).
					Str("service", serviceName).
					Str("subject", subject).
					Msg("failed to publish log line")
			}

		case <-ticker.C:
			a.mu.RLock()
			_, exists := a.processes[serviceName]
			a.mu.RUnlock()

			if !exists {
				log.Info().
					Str("service", serviceName).
					Msg("process no longer exists, ending log stream")
				cancel()
				return
			}

		case <-ctx.Done():
			return
		}
	}
}
