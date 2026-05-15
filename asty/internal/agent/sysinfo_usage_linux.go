//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
)

// detectMemoryAvailableMB reads MemAvailable from /proc/meminfo (in MB).
// MemAvailable is what `free -m` reports as "available" — accounts for
// reclaimable page cache, not just MemFree — so it's the right number
// to compare against MemTotal for "honest memory pressure". Falls back
// to 0 if /proc/meminfo can't be read; nodeinfo treats 0 as unknown.
func detectMemoryAvailableMB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					return kb / 1024
				}
			}
		}
	}
	return 0
}

// detectCPUUsedMHz returns currently-consumed CPU in MHz, system-wide,
// computed from the delta between two /proc/stat aggregate-cpu reads.
// The aggregate "cpu" line is: cpu user nice system idle iowait irq
// softirq steal guest guest_nice — all in clock ticks. Sum of all
// fields is total; idle+iowait counts as idle.
func detectCPUUsedMHz(totalMHz int) int {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}
	var totalNow, idleNow uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		totalNow += v
		if i == 3 || i == 4 {
			idleNow += v
		}
	}
	return cpuUsedFromTimings(totalNow, idleNow, totalMHz)
}
