package scenario

import (
	"fmt"

	"github.com/arloliu/fuda"
)

// LoadFromFile loads a scenario from a YAML file using fuda for parsing.
func LoadFromFile(path string) (*Scenario, error) {
	var s Scenario
	if err := fuda.LoadFile(path, &s); err != nil {
		return nil, fmt.Errorf("failed to load scenario file: %w", err)
	}

	if s.Name == "" {
		return nil, fmt.Errorf("scenario name is required")
	}

	return &s, nil
}
