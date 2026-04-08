package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	defaultBaseDir     = "centerpiece/tickets"
	defaultAgentMuxBin = "agent-mux"
	defaultMaxRetry    = 3
	defaultProfile     = "jenkins-junior"
)

type Config struct {
	BaseDir     string   `toml:"base_dir"`
	AgentMuxBin string   `toml:"agent_mux_bin"`
	MaxRetry    int      `toml:"max_retry"`
	Defaults    Defaults `toml:"defaults"`
}

type Defaults struct {
	Engine  string   `toml:"engine"`
	Model   string   `toml:"model"`
	Effort  string   `toml:"effort"`
	Profile string   `toml:"profile"`
	WorkDir string   `toml:"work_dir"`
	Skills  []string `toml:"default_skills"`
}

func Load() (Config, error) {
	cfg := Config{}

	path, err := findConfigFile()
	if err != nil {
		return Config{}, err
	}
	if path != "" {
		if _, err := toml.DecodeFile(path, &cfg); err != nil {
			return Config{}, err
		}
		// Resolve relative BaseDir against the config file's directory so the
		// binary works regardless of the caller's working directory.
		if cfg.BaseDir != "" && !filepath.IsAbs(cfg.BaseDir) {
			cfg.BaseDir = filepath.Join(filepath.Dir(path), cfg.BaseDir)
		}
	}

	if value := os.Getenv("TICKETS_BASE_DIR"); value != "" {
		cfg.BaseDir = value
	}
	if value := os.Getenv("TICKETS_AGENT_MUX_BIN"); value != "" {
		cfg.AgentMuxBin = value
	}

	if cfg.BaseDir == "" {
		cfg.BaseDir = defaultBaseDir
	}
	if cfg.AgentMuxBin == "" {
		cfg.AgentMuxBin = defaultAgentMuxBin
	}
	if cfg.MaxRetry == 0 {
		cfg.MaxRetry = defaultMaxRetry
	}
	if cfg.Defaults.Profile == "" {
		cfg.Defaults.Profile = defaultProfile
	}

	return cfg, nil
}

func findConfigFile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		path := filepath.Join(dir, ".tickets.toml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}
