package artifacts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// artifactDownloadTimeout caps the full request (dial + read body).
// Typical artifacts are 10–50 MiB; even on a 100 KiB/s link that's
// ~10 minutes worst case. A hung CDN past this is treated as failure
// and the controller retries on the next reconcile.
const artifactDownloadTimeout = 10 * time.Minute

// Downloader handles downloading and extracting service artifacts
type Downloader struct {
	client *http.Client
}

// NewDownloader creates a new artifact downloader
func NewDownloader() *Downloader {
	return &Downloader{
		client: &http.Client{Timeout: artifactDownloadTimeout},
	}
}

// Download downloads and extracts an artifact to the destination directory
func (d *Downloader) Download(url, checksum, destDir string) error {
	if strings.HasPrefix(url, "file://") || url == "local" {
		return d.copyLocal(url, destDir)
	}

	log.Info().
		Str("url", url).
		Str("dest", destDir).
		Msg("downloading artifact")

	tmpFile, err := os.CreateTemp("", "asty-artifact-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	resp, err := d.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("failed to write artifact: %w", err)
	}

	calculatedChecksum := hex.EncodeToString(hash.Sum(nil))
	expectedChecksum := strings.TrimPrefix(checksum, "sha256:")

	if calculatedChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculatedChecksum)
	}

	log.Info().
		Str("checksum", calculatedChecksum).
		Msg("artifact checksum verified")

	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek temp file: %w", err)
	}

	if err := d.extract(tmpFile, destDir); err != nil {
		return fmt.Errorf("failed to extract artifact: %w", err)
	}

	log.Info().
		Str("dest", destDir).
		Msg("artifact extracted successfully")

	return nil
}

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

func (d *Downloader) copyLocal(url, destDir string) error {
	serviceName := filepath.Base(destDir)

	var sourcePath string
	if strings.HasPrefix(url, "file://") {
		sourcePath = strings.TrimPrefix(url, "file://")
	} else {
		sourcePath = filepath.Join("bin", serviceName)
	}

	log.Info().
		Str("source", sourcePath).
		Str("dest", destDir).
		Msg("copying local binary")

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create dest dir: %w", err)
	}

	src, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source binary: %w", err)
	}
	defer src.Close()

	destPath := filepath.Join(destDir, serviceName)
	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create dest binary: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	log.Info().
		Str("dest", destPath).
		Msg("local binary copied successfully")

	return nil
}
