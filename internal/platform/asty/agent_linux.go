//go:build linux

package asty

import (
	"os"
	"strconv"
	"strings"
)

func detectCPUMHz() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 4000
	}

	var totalMHz float64
	var cores int
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					totalMHz += mhz
					cores++
				}
			}
		}
	}

	if cores == 0 {
		return 4000
	}
	return int(totalMHz)
}

func detectMemoryMB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 8192
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return kb / 1024
				}
			}
		}
	}

	return 8192
}
