package agent

import (
	"os"
	"strings"
	"sync"

	"asty/asty/internal/core/types"
)

// defaultDiskOSBaselineRatio is the fallback share of the fake disk
// counted as pre-occupied by the simulated OS when neither
// cfg.Agent.Capacity.DiskOSBaseline nor any other override is set.
// 20% on a 20 GB fake disk = 4 GB — realistic for a minimal Linux
// install.
const defaultDiskOSBaselineRatio = 0.20

// defaultNATSBaselineMB is the fallback for
// cfg.Agent.Capacity.NATSDiskBaseline — matches a stock nats-server
// binary on disk.
const defaultNATSBaselineMB = 30

// diskOSBaselineMB returns the fake-OS baseline used in dev disk
// projection: cfg.Agent.Capacity.DiskOSBaseline (absolute MB) when
// the caller passes a non-negative override, otherwise
// defaultDiskOSBaselineRatio × fakeTotal.
func diskOSBaselineMB(fakeTotal, override int64) int64 {
	if override >= 0 {
		return override
	}
	return int64(float64(fakeTotal) * defaultDiskOSBaselineRatio)
}

// natsDiskBaselineMB returns the synthesized NATS binary footprint:
// cfg.Agent.Capacity.NATSDiskBaseline when the caller passes a
// non-negative override, otherwise defaultNATSBaselineMB.
func natsDiskBaselineMB(override int64) int64 {
	if override >= 0 {
		return override
	}
	return defaultNATSBaselineMB
}

// astyBinarySizeMB returns the size of the running asty binary, in MB.
// Used by the dev disk-projection in nodeinfo.go to count the agent's
// own footprint (which lives outside work_dir, so dirSizeMB misses it).
// Returns 0 on any error — the projection then under-counts Asty,
// which is preferable to skipping the projection entirely.
func astyBinarySizeMB() int64 {
	p, err := os.Executable()
	if err != nil {
		return 0
	}
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Size() / (1024 * 1024)
}

// cpuTimings stores the previous /proc/stat (or kern.cp_time) snapshot
// so detectCPUUsedMHz can compute usage from the delta. First call
// after process start has no prior snapshot and must return 0;
// subsequent heartbeats see real values. Mutex-protected so concurrent
// callers (heartbeat + on-demand reads) don't race the counters.
var cpuTimings struct {
	mu      sync.Mutex
	total   uint64
	idle    uint64
	hasPrev bool
}

// cpuUsedFromTimings turns a (total, idle) tick-counter snapshot into
// "MHz currently consumed" by deltaing against the previous snapshot.
// The platform-specific detectCPUUsedMHz just fetches the numbers and
// hands them here, keeping the delta bookkeeping in one place.
func cpuUsedFromTimings(totalNow, idleNow uint64, totalMHz int) int {
	cpuTimings.mu.Lock()
	defer cpuTimings.mu.Unlock()
	defer func() {
		cpuTimings.total = totalNow
		cpuTimings.idle = idleNow
		cpuTimings.hasPrev = true
	}()
	if !cpuTimings.hasPrev {
		return 0
	}
	dt := totalNow - cpuTimings.total
	di := idleNow - cpuTimings.idle
	if dt == 0 {
		return 0
	}
	usedRatio := 1.0 - float64(di)/float64(dt)
	if usedRatio < 0 {
		usedRatio = 0
	}
	return int(float64(totalMHz) * usedRatio)
}

// normaliseDiskType maps free-form values (uppercase, hdd/ssd/spinning/
// nvme, etc.) onto the typed DiskType constants. Anything we don't
// recognise becomes DiskUnknown rather than a silent typo. Used by
// both platform-specific detectDiskType implementations and the
// A_DISK_TYPE env override path.
func normaliseDiskType(raw string) types.DiskType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ssd", "nvme", "flash":
		return types.DiskSSD
	case "hdd", "spinning", "rotational":
		return types.DiskHDD
	default:
		return types.DiskUnknown
	}
}
