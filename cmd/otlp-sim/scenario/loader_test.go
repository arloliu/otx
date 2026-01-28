package scenario

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_ValidYAML(t *testing.T) {
	// Create a temporary YAML file
	yamlContent := `
name: test-scenario
description: A test scenario
services:
  - name: test-service
rootSpan:
  name: "GET /test"
  service: test-service
  kind: SERVER
  duration: "100ms"
  attributes:
    http.method: GET
  children:
    - name: child-span
      service: test-service
      kind: INTERNAL
      duration: "50ms"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test-scenario.yaml")
	err := os.WriteFile(filePath, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	// Load the scenario
	s, err := LoadFromFile(filePath)
	require.NoError(t, err)

	// Verify the scenario
	assert.Equal(t, "test-scenario", s.Name)
	assert.Equal(t, "A test scenario", s.Description)
	assert.Len(t, s.Services, 1)
	assert.Equal(t, "test-service", s.Services[0].Name)

	// Verify root span
	assert.Equal(t, "GET /test", s.RootSpan.Name)
	assert.Equal(t, SpanKindServer, s.RootSpan.Kind)
	assert.Equal(t, "GET", s.RootSpan.Attributes["http.method"])

	// Verify child span
	require.Len(t, s.RootSpan.Children, 1)
	assert.Equal(t, "child-span", s.RootSpan.Children[0].Name)
}

func TestLoadFromFile_WithLogs(t *testing.T) {
	yamlContent := `
name: log-scenario
description: Scenario with logs
services:
  - name: log-service
rootSpan:
  name: "test-span"
  service: log-service
  kind: SERVER
  duration: "50ms"
  logs:
    - level: INFO
      message: "Request received"
    - level: ERROR
      message: "Something went wrong"
      attributes:
        error.code: "500"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "log-scenario.yaml")
	err := os.WriteFile(filePath, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	s, err := LoadFromFile(filePath)
	require.NoError(t, err)

	require.Len(t, s.RootSpan.Logs, 2)
	assert.Equal(t, "INFO", s.RootSpan.Logs[0].Level)
	assert.Equal(t, "Request received", s.RootSpan.Logs[0].Message)
	assert.Equal(t, "ERROR", s.RootSpan.Logs[1].Level)
	assert.Equal(t, "500", s.RootSpan.Logs[1].Attributes["error.code"])
}

func TestLoadFromFile_WithErrorSimulation(t *testing.T) {
	yamlContent := `
name: error-scenario
description: Scenario with error simulation
services:
  - name: error-service
rootSpan:
  name: "error-span"
  service: error-service
  kind: SERVER
  duration: "100ms"
  errorRate: 0.1
  errorStatus: "simulated failure"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "error-scenario.yaml")
	err := os.WriteFile(filePath, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	s, err := LoadFromFile(filePath)
	require.NoError(t, err)

	assert.Equal(t, 0.1, s.RootSpan.ErrorRate)
	assert.Equal(t, "simulated failure", s.RootSpan.ErrorStatus)
}

func TestLoadFromFile_FileNotFound(t *testing.T) {
	s, err := LoadFromFile("/non/existent/path.yaml")
	assert.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "failed to load scenario file")
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	yamlContent := `
name: broken
description: [invalid yaml
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.yaml")
	err := os.WriteFile(filePath, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	s, err := LoadFromFile(filePath)
	assert.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "failed to load scenario file")
}

func TestLoadFromFile_MissingName(t *testing.T) {
	yamlContent := `
description: Missing name field
services:
  - name: test-service
rootSpan:
  name: "test"
  service: test-service
  kind: SERVER
  duration: "100ms"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "no-name.yaml")
	err := os.WriteFile(filePath, []byte(yamlContent), 0o644)
	require.NoError(t, err)

	s, err := LoadFromFile(filePath)
	assert.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "scenario name is required")
}
