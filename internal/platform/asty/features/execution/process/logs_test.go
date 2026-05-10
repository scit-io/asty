package process

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"asty/internal/platform/asty/core/types"
)

// splitLines splits a string into lines, optionally returning only the last N lines.
func splitLines(data string, lastN int) []string {
	if data == "" {
		return []string{}
	}

	lines := []string{}
	current := ""

	for _, ch := range data {
		if ch == '\n' {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}

	if current != "" {
		lines = append(lines, current)
	}

	if lastN > 0 && len(lines) > lastN {
		return lines[len(lines)-lastN:]
	}

	return lines
}

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

	// Test splitLines
	lines := splitLines(logStr, 0)
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines, got %d", len(lines))
	}

	// Test splitLines with limit
	lastTwo := splitLines(logStr, 2)
	if len(lastTwo) > 2 {
		t.Errorf("expected max 2 lines, got %d", len(lastTwo))
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

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		lastN int
		want  int
	}{
		{
			name:  "empty string",
			input: "",
			lastN: 0,
			want:  0,
		},
		{
			name:  "single line",
			input: "line1\n",
			lastN: 0,
			want:  1,
		},
		{
			name:  "multiple lines",
			input: "line1\nline2\nline3\n",
			lastN: 0,
			want:  3,
		},
		{
			name:  "last 2 lines",
			input: "line1\nline2\nline3\nline4\n",
			lastN: 2,
			want:  2,
		},
		{
			name:  "no trailing newline",
			input: "line1\nline2",
			lastN: 0,
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitLines(tt.input, tt.lastN)
			if len(result) != tt.want {
				t.Errorf("splitLines() got %d lines, want %d", len(result), tt.want)
			}
		})
	}
}
