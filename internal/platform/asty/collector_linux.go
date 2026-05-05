//go:build linux

package asty

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// getCPUPercent reads CPU usage from /proc/[pid]/stat
func (mc *MetricsCollector) getCPUPercent(pid int) (float64, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	s := string(data)
	closeParenIdx := strings.LastIndex(s, ")")
	if closeParenIdx < 0 || closeParenIdx+2 >= len(s) {
		return 0, fmt.Errorf("invalid stat format")
	}
	fields := strings.Fields(s[closeParenIdx+2:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("invalid stat format: not enough fields")
	}

	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}

	totalTicks := utime + stime

	mc.mu.Lock()
	prev, hasPrev := mc.prevTicks[pid]
	mc.prevTicks[pid] = totalTicks
	mc.mu.Unlock()

	if !hasPrev {
		return 0, nil
	}

	deltaTicks := totalTicks - prev
	clkTck := uint64(100)
	intervalSec := uint64(mc.interval.Seconds())
	if intervalSec == 0 {
		intervalSec = 1
	}

	maxTicks := clkTck * intervalSec
	if maxTicks == 0 {
		return 0, nil
	}

	cpuPercent := float64(deltaTicks) / float64(maxTicks) * 100.0
	if cpuPercent > 100.0 {
		cpuPercent = 100.0
	}

	return cpuPercent, nil
}

// getMemoryMB reads memory usage from /proc/[pid]/status
func (mc *MetricsCollector) getMemoryMB(pid int) (int64, error) {
	statusPath := fmt.Sprintf("/proc/%d/status", pid)
	data, err := os.ReadFile(statusPath)
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					return 0, err
				}
				return kb / 1024, nil
			}
		}
	}

	return 0, fmt.Errorf("VmRSS not found")
}
