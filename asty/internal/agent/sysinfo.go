package agent

import (
	"strings"

	"asty/asty/internal/core/types"
)

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
