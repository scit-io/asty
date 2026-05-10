//go:build darwin

package agent

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

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
