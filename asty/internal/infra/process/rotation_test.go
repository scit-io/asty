package process

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestRotatingWriterChain writes data in increments that span the rollover
// threshold and verifies the .1/.2/... chain stays bounded by maxFiles.
func TestRotatingWriterChain(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	rw, err := newRotatingWriter(logPath, 1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	// Write 600 B — below the 1 KiB threshold, no rotation expected.
	if _, err := rw.Write(bytes.Repeat([]byte("A"), 600)); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, logPath+".1")

	// Write another 600 B. Cumulative 1200 B > 1024 → rotation,
	// .1 holds the original "A"s and active gets the new "B"s.
	if _, err := rw.Write(bytes.Repeat([]byte("B"), 600)); err != nil {
		t.Fatal(err)
	}
	mustExist(t, logPath+".1")
	if b, _ := os.ReadFile(logPath + ".1"); !bytes.Equal(b, bytes.Repeat([]byte("A"), 600)) {
		t.Errorf(".1 should hold the 600 A's, got %d bytes (first=%c)", len(b), b[0])
	}

	// Two more 600-B writes → second rotation creates .2.
	rw.Write(bytes.Repeat([]byte("C"), 600))
	rw.Write(bytes.Repeat([]byte("D"), 600))
	mustExist(t, logPath+".2")

	// Two more → third rotation, but .3 must NOT appear (maxFiles=2).
	rw.Write(bytes.Repeat([]byte("E"), 600))
	rw.Write(bytes.Repeat([]byte("F"), 600))
	mustNotExist(t, logPath+".3")
}

// TestRotatingWriterOversizedWrite covers the corner case where a single
// Write is larger than maxBytes against an empty active file: it should
// land in the active file intact rather than spawning an empty rotation.
func TestRotatingWriterOversizedWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "test.log")

	rw, err := newRotatingWriter(logPath, 100, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()

	big := bytes.Repeat([]byte("X"), 500)
	if _, err := rw.Write(big); err != nil {
		t.Fatal(err)
	}
	mustNotExist(t, logPath+".1")
	if b, _ := os.ReadFile(logPath); len(b) != 500 {
		t.Errorf("active file should hold the oversized 500 B write, got %d", len(b))
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to exist: %v", filepath.Base(path), err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to not exist (err=%v)", filepath.Base(path), err)
	}
}
