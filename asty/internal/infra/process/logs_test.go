package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asty/asty/internal/core/types"
)

func TestProcessLogs(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "asty-logs-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create service definition that writes logs
	svc := &types.ServiceDefinition{
		Name:    "test-logger",
		Command: "/bin/sh -c 'echo line1; echo line2; echo line3; sleep 1'",
		Env:     make(map[string]string),
		Resources: types.Resources{
			CPU:    100,
			Memory: 64,
		},
	}

	// Create process
	proc := New(svc, "test-node", tmpDir)

	// Start process
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := proc.Start(ctx); err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// Wait for process to write logs and complete
	time.Sleep(2 * time.Second)

	// Get logs
	logs, err := proc.GetLogs(100)
	if err != nil {
		t.Fatalf("failed to get logs: %v", err)
	}

	// Check logs content
	logStr := string(logs)
	if logStr == "" {
		t.Error("expected logs, got empty string")
	}

	if !strings.Contains(logStr, "line1") {
		t.Errorf("expected 'line1' in logs, got: %s", logStr)
	}
	if !strings.Contains(logStr, "line2") {
		t.Errorf("expected 'line2' in logs, got: %s", logStr)
	}
	if !strings.Contains(logStr, "line3") {
		t.Errorf("expected 'line3' in logs, got: %s", logStr)
	}

	// Bounded tail: ask for 2 lines, should get line2+line3 only.
	tailBytes, err := proc.GetLogs(2)
	if err != nil {
		t.Fatalf("failed to get bounded logs: %v", err)
	}
	tail := string(tailBytes)
	if strings.Contains(tail, "line1") {
		t.Errorf("GetLogs(2) should not include line1, got: %s", tail)
	}
	if !strings.Contains(tail, "line2") || !strings.Contains(tail, "line3") {
		t.Errorf("GetLogs(2) should include line2 and line3, got: %s", tail)
	}

	// Stop process
	if err := proc.Stop(); err != nil {
		t.Fatalf("failed to stop process: %v", err)
	}

	// Verify log file exists
	logPath := filepath.Join(tmpDir, "logs", "test-logger.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("log file should exist at %s", logPath)
	}
}

// TestGetLogsTail writes a file with deterministic content and verifies
// GetLogs returns exactly the requested tail, including across the 4 KiB
// chunk boundary the backward scan uses.
func TestGetLogsTail(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "asty-getlogs-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	svc := &types.ServiceDefinition{Name: "tail-svc"}
	proc := New(svc, "test-node", tmpDir)
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	// 5000 short lines — total size comfortably crosses the 4 KiB chunk
	// boundary, so the backward scan must stitch chunks correctly.
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "line%d\n", i)
	}
	if err := os.WriteFile(proc.GetLogPath(), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		lines     int
		wantFirst string
		wantLast  string
		wantCount int
	}{
		{"tail 3", 3, "line4997", "line4999", 3},
		{"tail 100", 100, "line4900", "line4999", 100},
		{"tail across chunk boundary", 1000, "line4000", "line4999", 1000},
		{"tail more than file has", 10000, "line0", "line4999", 5000},
		{"tail 0 returns whole file", 0, "line0", "line4999", 5000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := proc.GetLogs(tt.lines)
			if err != nil {
				t.Fatalf("GetLogs: %v", err)
			}
			got := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
			if len(got) != tt.wantCount {
				t.Fatalf("got %d lines, want %d", len(got), tt.wantCount)
			}
			if got[0] != tt.wantFirst {
				t.Errorf("first line = %q, want %q", got[0], tt.wantFirst)
			}
			if got[len(got)-1] != tt.wantLast {
				t.Errorf("last line = %q, want %q", got[len(got)-1], tt.wantLast)
			}
		})
	}
}
