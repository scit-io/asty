//go:build darwin

package agent

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"asty/asty/internal/core/types"

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

	out, err := exec.Command("sysctl", "-n", "hw.cpufrequency").Output()
	if err != nil {
		out, err = exec.Command("sysctl", "-n", "hw.cpufrequency_max").Output()
		if err != nil {
			return runtime.NumCPU() * 2000
		}
	}

	hz, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return runtime.NumCPU() * 2000
	}

	mhzPerCore := int(hz / 1_000_000)
	return runtime.NumCPU() * mhzPerCore
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

	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 8192
	}

	bytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 8192
	}

	return bytes / (1024 * 1024)
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
	bsize := int64(st.Bsize)
	total = int64(st.Blocks) * bsize / mib
	available = int64(st.Bavail) * bsize / mib
	return total, available
}

// detectSwapMB reports system swap (sysctl vm.swapusage) total +
// available in MB. Falls back to (0, 0) on failure. Override applied
// at caller — see detectDiskMB note.
func detectSwapMB() (total, available int64) {
	out, err := exec.Command("sysctl", "-n", "vm.swapusage").Output()
	if err != nil {
		return 0, 0
	}
	// Format: "total = 5120.00M  used = 3168.00M  free = 1952.00M  (encrypted)"
	fields := strings.Fields(string(out))
	var used int64
	for i, f := range fields {
		switch f {
		case "total":
			total = parseSwapField(fields, i)
		case "used":
			used = parseSwapField(fields, i)
		case "free":
			available = parseSwapField(fields, i)
		}
	}
	if available == 0 && total > 0 {
		available = total - used
	}
	return total, available
}

// parseSwapField pulls "5120.00M" out of vm.swapusage's "k = vvvv.vvU"
// triplet at index i (the key). Drops the trailing M/G suffix.
func parseSwapField(fields []string, i int) int64 {
	if i+2 >= len(fields) {
		return 0
	}
	v := strings.TrimRight(fields[i+2], "MG")
	mult := int64(1)
	if strings.HasSuffix(fields[i+2], "G") {
		mult = 1024
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return int64(f) * mult
	}
	return 0
}

// detectDiskType reports the physical class (ssd | hdd | unknown) of
// the disk hosting the agent work_dir. On macOS we default to ssd —
// every shipping Mac since 2018 is solid-state, and parsing diskutil
// output reliably is more work than it's worth. A_DISK_TYPE overrides.
func detectDiskType() types.DiskType {
	if override := os.Getenv("A_DISK_TYPE"); override != "" {
		log.Info().Str("disk_type", override).Msg("using DiskType override from A_DISK_TYPE")
		return normaliseDiskType(override)
	}
	return types.DiskSSD
}

