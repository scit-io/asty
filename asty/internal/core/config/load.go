package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// DefaultPath is the relative path the agent and server consult when
// the operator did not pass -config.
const DefaultPath = "./config.asty"

// Load builds a Config from defaults, the YAML file at path, and
// finally environment overrides. The same yaml.Unmarshal pattern as
// ops/deployer/loader.go: read file → unmarshal into the
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
		// Expand ${VAR} references inside YAML values so secrets can
		// live in env (dev.vars in dev, real env in prod) instead of
		// being inlined in the checked-in YAML. Bare $NAME is left
		// alone on purpose — NATS subjects like "$SYS.REQ.SERVER.*"
		// and "$SRV.PING.*" use $ as a literal namespace prefix.
		expanded := []byte(expandBracedEnv(string(data)))
		if err := yaml.Unmarshal(expanded, cfg); err != nil {
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

var bracedEnvRE = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expandBracedEnv(s string) string {
	return bracedEnvRE.ReplaceAllStringFunc(s, func(match string) string {
		return os.Getenv(match[2 : len(match)-1])
	})
}
