package agent

import (
	"io/fs"
	"path/filepath"
)

// dirSizeMB sums the on-disk size of every regular file under root,
// returning the total in MB. Walk errors are silently absorbed: missing
// or unreadable subtrees just don't contribute. Symlinks are skipped to
// keep accounting stable when the workdir contains alias mounts.
func dirSizeMB(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total / (1024 * 1024)
}
