package deployment

import (
	"os"
	"path/filepath"
	"testing"

	"asty/internal/platform/asty/core/types"
)

func TestLoadServiceDefinition(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	servicePath := filepath.Join(tmpDir, "test.asty")

	// Write test service definition
	content := `name: test-service
type: service
artifact:
  url: https://example.com/test.tar.gz
  checksum: sha256:abc123
command: ./test
user: asty
kill_timeout: 30s
env:
  TEST_VAR: "value"
resources:
  cpu: 100
  memory: 64
health:
  type: http
  path: /health
  interval: 10s
  timeout: 3s
logs:
  max_files: 5
  max_file_size: 10
update:
  max_parallel: 1
  min_healthy_time: 10s
  healthy_deadline: 3m
  auto_revert: true
restart:
  attempts: 10
  interval: 5m
  delay: 15s
`

	if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Load service definition
	svc, err := LoadServiceDefinition(servicePath)
	if err != nil {
		t.Fatalf("failed to load service definition: %v", err)
	}

	// Verify fields
	if svc.Name != "test-service" {
		t.Errorf("expected name 'test-service', got '%s'", svc.Name)
	}

	if svc.Type != types.ServiceTypeService {
		t.Errorf("expected type 'service', got '%s'", svc.Type)
	}

	if svc.Artifact.URL != "https://example.com/test.tar.gz" {
		t.Errorf("unexpected artifact URL: %s", svc.Artifact.URL)
	}

	if svc.Resources.CPU != 100 {
		t.Errorf("expected CPU 100, got %d", svc.Resources.CPU)
	}

	if svc.Resources.Memory != 64 {
		t.Errorf("expected Memory 64, got %d", svc.Resources.Memory)
	}

	if svc.Health.Type != "http" {
		t.Errorf("expected health type 'http', got '%s'", svc.Health.Type)
	}

	if svc.Env["TEST_VAR"] != "value" {
		t.Errorf("expected TEST_VAR='value', got '%s'", svc.Env["TEST_VAR"])
	}
}

func TestServiceDefinitionValidation(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "missing name",
			content: `type: service
command: ./test`,
			wantErr: true,
		},
		{
			name: "invalid type",
			content: `name: test
type: invalid
command: ./test`,
			wantErr: true,
		},
		{
			name: "valid system service",
			content: `name: test
type: system
command: ./test
user: root`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			servicePath := filepath.Join(tmpDir, tt.name+".asty")
			if err := os.WriteFile(servicePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			_, err := LoadServiceDefinition(servicePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadServiceDefinition() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
