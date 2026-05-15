//go:build darwin

package agent

import (
	"os/exec"
	"strconv"
	"strings"
)

// detectMemoryAvailableMB parses vm_stat output to report MB the OS
// considers free or trivially reclaimable. macOS doesn't expose a
// single MemAvailable equivalent — Activity Monitor's "Available"
// roughly equals Pages free + inactive + purgeable * pageSize. We use
// that. vm_stat normalises its output to 4096-byte pages on every
// Apple architecture (the header line "page size of 4096 bytes" is
// reliable even on Apple Silicon, where the kernel page is 16K).
func detectMemoryAvailableMB() int64 {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0
	}
	var free, inactive, purgeable int64
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Pages free:"):
			free = parsePagesField(fields)
		case strings.HasPrefix(line, "Pages inactive:"):
			inactive = parsePagesField(fields)
		case strings.HasPrefix(line, "Pages purgeable:"):
			purgeable = parsePagesField(fields)
		}
	}
	const pageSize = 4096
	return (free + inactive + purgeable) * pageSize / (1024 * 1024)
}

// parsePagesField extracts the integer page count from a vm_stat line
// like "Pages free:                       12345." (note trailing dot).
func parsePagesField(fields []string) int64 {
	if len(fields) == 0 {
		return 0
	}
	last := strings.TrimRight(fields[len(fields)-1], ".")
	v, _ := strconv.ParseInt(last, 10, 64)
	return v
}

// detectCPUUsedMHz returns currently-consumed CPU in MHz, system-wide,
// from the delta between two kern.cp_time reads. The sysctl emits five
// uint64 tick counters: user, nice, sys, intr, idle.
func detectCPUUsedMHz(totalMHz int) int {
	out, err := exec.Command("sysctl", "-n", "kern.cp_time").Output()
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(out))
	if len(fields) < 5 {
		return 0
	}
	var totalNow, idleNow uint64
	for i, f := range fields {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		totalNow += v
		if i == 4 {
			idleNow = v
		}
	}
	return cpuUsedFromTimings(totalNow, idleNow, totalMHz)
}
