package asty

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

	"github.com/rs/zerolog/log"
)

// ArtifactDownloader handles downloading and extracting service artifacts
type ArtifactDownloader struct {
	client *http.Client
}

// NewArtifactDownloader creates a new artifact downloader
func NewArtifactDownloader() *ArtifactDownloader {
	return &ArtifactDownloader{
		client: &http.Client{},
	}
}

// Download downloads and extracts an artifact to the destination directory
func (ad *ArtifactDownloader) Download(url, checksum, destDir string) error {
	log.Info().
		Str("url", url).
		Str("dest", destDir).
		Msg("downloading artifact")

	// Create temp file for download
	tmpFile, err := os.CreateTemp("", "asty-artifact-*.tar.gz")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Download artifact
	resp, err := ad.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Calculate checksum while downloading
	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		return fmt.Errorf("failed to write artifact: %w", err)
	}

	// Verify checksum
	calculatedChecksum := hex.EncodeToString(hash.Sum(nil))
	expectedChecksum := strings.TrimPrefix(checksum, "sha256:")

	if calculatedChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, calculatedChecksum)
	}

	log.Info().
		Str("checksum", calculatedChecksum).
		Msg("artifact checksum verified")

	// Extract archive
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek temp file: %w", err)
	}

	if err := ad.extract(tmpFile, destDir); err != nil {
		return fmt.Errorf("failed to extract artifact: %w", err)
	}

	log.Info().
		Str("dest", destDir).
		Msg("artifact extracted successfully")

	return nil
}

// extract extracts a tar.gz archive to the destination directory
func (ad *ArtifactDownloader) extract(r io.Reader, destDir string) error {
	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}

	// Open gzip reader
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close()

	// Open tar reader
	tr := tar.NewReader(gzr)

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct destination path
		destPath := filepath.Join(destDir, header.Name)

		// Security check: prevent path traversal
		if !strings.HasPrefix(destPath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid file path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

		case tar.TypeReg:
			// Extract file
			if err := ad.extractFile(tr, destPath, header.FileInfo().Mode()); err != nil {
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

// extractFile extracts a single file from tar archive
func (ad *ArtifactDownloader) extractFile(r io.Reader, destPath string, mode os.FileMode) error {
	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	// Create file
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer f.Close()

	// Copy content
	if _, err := io.Copy(f, r); err != nil {
		return err
	}

	return nil
}
