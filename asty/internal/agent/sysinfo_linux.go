//go:build linux

package agent

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"asty/asty/internal/core/types"

	"github.com/rs/zerolog/log"
)

// detectCPUMHz returns the host's aggregate CPU capacity in MHz, or
// the supplied override if non-zero. Override flows from
// cfg.Agent.Capacity.CPUTotal (env A_CPU_TOTAL via core/config).
func detectCPUMHz(override int) int {
	if override > 0 {
		log.Info().Int("cpu_mhz", override).Msg("using CPU override from cfg.Agent.Capacity.CPUTotal")
		return override
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

// detectMemoryMB returns the host's memory total in MB, or the
// supplied override if non-zero. Override flows from
// cfg.Agent.Capacity.MemoryTotal (env A_MEMORY_TOTAL via core/config).
func detectMemoryMB(override int64) int64 {
	if override > 0 {
		log.Info().Int64("memory_mb", override).Msg("using Memory override from cfg.Agent.Capacity.MemoryTotal")
		return override
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
// and available space in MB as the OS reports them. Falls back to
// (0, 0) if statfs fails. Override (A_DISK_TOTAL) is applied at the
// caller (nodeinfo.go) so that the real host values stay available for
// projecting host fullness onto the fake-disk envelope.
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
	return total, available
}

// detectSwapMB reads SwapTotal/SwapFree from /proc/meminfo and returns
// them in MB. Falls back to (0, 0) if the file is unreadable or both
// entries are missing. Override applied at caller — see detectDiskMB note.
func detectSwapMB() (total, available int64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		kb, perr := strconv.ParseInt(fields[1], 10, 64)
		if perr != nil {
			continue
		}
		switch fields[0] {
		case "SwapTotal:":
			total = kb / 1024
		case "SwapFree:":
			available = kb / 1024
		}
	}
	return total, available
}

// detectDiskType walks /sys/block to classify the host's storage. Any
// non-rotational device flips the result to SSD; we report HDD only
// when every block device reports rotational=1. This is a reasonable
// approximation in containerised dev too: the host's /sys is exposed
// read-only and reflects real hardware. Override flows from
// cfg.Agent.Capacity.DiskType (env A_DISK_TYPE via core/config).
func detectDiskType(override string) types.DiskType {
	if override != "" {
		log.Info().Str("disk_type", override).Msg("using DiskType override from cfg.Agent.Capacity.DiskType")
		return normaliseDiskType(override)
	}

	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return types.DiskUnknown
	}
	sawRot, sawNonRot := false, false
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		b, err := os.ReadFile("/sys/block/" + name + "/queue/rotational")
		if err != nil {
			continue
		}
		switch strings.TrimSpace(string(b)) {
		case "0":
			sawNonRot = true
		case "1":
			sawRot = true
		}
	}
	switch {
	case sawNonRot:
		return types.DiskSSD
	case sawRot:
		return types.DiskHDD
	default:
		return types.DiskUnknown
	}
}
