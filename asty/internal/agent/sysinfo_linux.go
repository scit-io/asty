//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
)

func detectCPUMHz() int {
	if override := os.Getenv("A_CPU_TOTAL"); override != "" {
		log.Debug().Str("A_CPU_TOTAL", override).Msg("cpu override env detected")
		if val, err := strconv.Atoi(override); err == nil && val > 0 {
			log.Info().Int("cpu_mhz", val).Msg("using CPU override from A_CPU_TOTAL")
			return val
		} else {
			log.Warn().Str("A_CPU_TOTAL", override).Err(err).Msg("failed to parse A_CPU_TOTAL")
		}
	}

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
	if override := os.Getenv("A_MEMORY_TOTAL"); override != "" {
		log.Debug().Str("A_MEMORY_TOTAL", override).Msg("memory override env detected")
		if val, err := strconv.ParseInt(override, 10, 64); err == nil && val > 0 {
			log.Info().Int64("memory_mb", val).Msg("using Memory override from A_MEMORY_TOTAL")
			return val
		} else {
			log.Warn().Str("A_MEMORY_TOTAL", override).Err(err).Msg("failed to parse A_MEMORY_TOTAL")
		}
	}

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

// detectDiskMB inspects the filesystem hosting `path` and reports total
// and available space in MB. Falls back to (0, 0) if statfs fails — the
// UI renders empty tiles instead of bogus numbers in that case.
// A_DISK_TOTAL overrides the total, mirroring A_CPU_TOTAL / A_MEMORY_TOTAL.
func detectDiskMB(path string) (total, available int64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		log.Warn().Err(err).Str("path", path).Msg("statfs failed; disk metrics unavailable")
		return 0, 0
	}
	const mib = 1024 * 1024
	bsize := st.Bsize
	total = int64(st.Blocks) * bsize / mib
	available = int64(st.Bavail) * bsize / mib

	if override := os.Getenv("A_DISK_TOTAL"); override != "" {
		if val, err := strconv.ParseInt(override, 10, 64); err == nil && val > 0 {
			log.Info().Int64("disk_mb", val).Msg("using Disk override from A_DISK_TOTAL")
			total = val
		}
	}
	return total, available
}
