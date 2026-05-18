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

// extract walks a gzipped tar stream into destDir, refusing entries
// whose resolved path escapes destDir (directory traversal guard).
// Returns the first error encountered, including malformed archives;
// callers are expected to clean up partial extractions themselves.
func (d *Downloader) extract(r io.Reader, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
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
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			if err := d.extractFile(tr, destPath, header.FileInfo().Mode()); err != nil {
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

// extractFile writes a single tar entry to destPath with the provided
// mode bits. Parent directories are created as needed; the file is
// truncated if it already exists.
func (d *Downloader) extractFile(r io.Reader, destPath string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return err
	}

	return nil
}
