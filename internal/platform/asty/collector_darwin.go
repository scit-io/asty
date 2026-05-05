//go:build darwin

package asty

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// getCPUPercent uses ps to get CPU usage on macOS
func (mc *MetricsCollector) getCPUPercent(pid int) (float64, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=").Output()
	if err != nil {
		return 0, fmt.Errorf("ps failed for pid %d: %w", pid, err)
	}

	val := strings.TrimSpace(string(out))
	if val == "" {
		return 0, fmt.Errorf("no cpu data for pid %d", pid)
	}

	cpuPercent, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, err
	}

	return cpuPercent, nil
}

// getMemoryMB uses ps to get RSS on macOS (ps reports RSS in KB on darwin)
func (mc *MetricsCollector) getMemoryMB(pid int) (int64, error) {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "rss=").Output()
	if err != nil {
		return 0, fmt.Errorf("ps failed for pid %d: %w", pid, err)
	}

	val := strings.TrimSpace(string(out))
	if val == "" {
		return 0, fmt.Errorf("no rss data for pid %d", pid)
	}

	kb, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, err
	}

	return kb / 1024, nil
}
