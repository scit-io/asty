package artifact

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// artifactDirMode is the permission Asty puts on every directory it
// creates while extracting an artifact. 0700 — owner-only — because
// agents share a host with other services and we don't want an
// `ls /var/lib/asty/<node>/<svc>/` from another uid to fingerprint
// what's deployed. TZ §10.2.
const artifactDirMode os.FileMode = 0o700

// artifactBinMode is the permission Asty applies to the extracted
// binary BEFORE `chmod +x` (agent's spawn path does that). 0400 keeps
// it read-only-by-owner; the exec bit gets stamped on right before
// process.Start. TZ §10.2.
const artifactBinMode os.FileMode = 0o400

// extract walks a gzipped tar stream into destDir, refusing entries
// whose resolved path escapes destDir (directory traversal guard).
// Returns the first error encountered, including malformed archives;
// callers are expected to clean up partial extractions themselves.
//
// Permissions: directories 0700, regular files 0400, regardless of
// what the archive itself encodes. The agent's spawn path stamps
// the exec bit on the named binary just before starting the process.
func (d *Downloader) extract(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, artifactDirMode); err != nil {
		return err
	}

	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		destPath := filepath.Join(destDir, header.Name)

		if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, artifactDirMode); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			if err := d.extractFile(tr, destPath); err != nil {
				return fmt.Errorf("failed to extract file %s: %w", header.Name, err)
			}

		default:
			log.Warn().
				Str("name", header.Name).
				Uint8("type", header.Typeflag).
				Msg("unsupported file type in archive")
		}
	}

	return nil
}

// extractFile writes a single tar entry to destPath with the fixed
// artifactBinMode permission, ignoring the mode the archive carries.
// Parent directories are created as needed (also 0700); the file is
// truncated if it already exists.
func (d *Downloader) extractFile(r io.Reader, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), artifactDirMode); err != nil {
		return err
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, artifactBinMode)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return err
	}

	return nil
}
