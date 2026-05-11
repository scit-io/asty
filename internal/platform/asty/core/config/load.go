package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultPath is the relative path the agent and server consult when
// the operator did not pass -config.
const DefaultPath = "./config.asty"

// Load builds a Config from defaults, the YAML file at path, and
// finally environment overrides. The same yaml.Unmarshal pattern as
// features/deployment/loader.go: read file → unmarshal into the
// pre-populated struct → env overrides on top.
//
// path == "" falls back to DefaultPath. An explicit -config path that
// does not exist is an error; a missing DefaultPath is tolerated so
// fully-env-driven deployments still work.
//
// Order of precedence (last wins): defaults → file → env vars.
func Load(path string) (*Config, error) {
	cfg := defaults()

	file := path
	explicit := path != ""
	if !explicit {
		file = DefaultPath
	}

	data, err := os.ReadFile(file)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}
	case errors.Is(err, fs.ErrNotExist) && !explicit:
		// fall through — defaults + env only
	default:
		return nil, fmt.Errorf("read %s: %w", file, err)
	}

	applyEnvOverrides(cfg)
	return cfg, nil
}
