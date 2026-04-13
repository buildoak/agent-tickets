package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

const (
	defaultBaseDir     = "centerpiece/tickets"
	defaultAgentMuxBin = "agent-mux"
	defaultMaxRetry    = 3
	defaultProfile     = "jenkins-junior"
)

type Config struct {
	BaseDir            string            `toml:"base_dir"`
	AgentMuxBin        string            `toml:"agent_mux_bin"`
	MaxRetry           int               `toml:"max_retry"`
	StaggerSeconds     int               `toml:"stagger_seconds"`
	MaxDispatchPerTick int               `toml:"max_dispatch_per_tick"`
	SkillPath          string            `toml:"skill_path"`
	Defaults           Defaults          `toml:"defaults"`
	Concurrency        map[string]int    `toml:"concurrency"`
	ModelWeight        map[string]int    `toml:"model_weight"`
	ProfileEngine      map[string]string `toml:"profile_engine"`
	ProfileModel       map[string]string `toml:"profile_model"`
	StallTimeoutMin    StallTimeoutMin              `toml:"stall_timeout_minutes"`
	Initiatives        map[string]InitiativeConfig  `toml:"initiatives"`
	Guardian           GuardianConfig               `toml:"guardian"`
}

type Defaults struct {
	Engine  string `toml:"engine"`
	Model   string `toml:"model"`
	Effort  string `toml:"effort"`
	Profile string `toml:"profile"`
}

type StallTimeoutMin struct {
	Worker int `toml:"worker"`
	Deep   int `toml:"deep"`
	Heavy  int `toml:"heavy"`
}

type GuardianConfig struct {
	Engine     string `toml:"engine"`
	Model      string `toml:"model"`
	Effort     string `toml:"effort"`
	Profile    string `toml:"profile"`
	Initiative string `toml:"initiative"`
}

type InitiativeConfig struct {
	DefaultProfile      string `toml:"default_profile"`
	DefaultTier         string `toml:"default_tier"`
	StallTimeoutMinutes int    `toml:"stall_timeout_minutes"`
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
	if value := os.Getenv("TICKETS_STAGGER_SECONDS"); value != "" {
		if v, err := strconv.Atoi(value); err == nil {
			cfg.StaggerSeconds = v
		}
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
	if cfg.MaxDispatchPerTick <= 0 {
		cfg.MaxDispatchPerTick = 1
	}
	if cfg.StallTimeoutMin.Worker <= 0 {
		cfg.StallTimeoutMin.Worker = 30
	}
	if cfg.StallTimeoutMin.Deep <= 0 {
		cfg.StallTimeoutMin.Deep = 45
	}
	if cfg.StallTimeoutMin.Heavy <= 0 {
		cfg.StallTimeoutMin.Heavy = 60
	}

	return cfg, nil
}

// ModelWeightFor returns the weight for a model name. Returns 1 if not configured.
func (c Config) ModelWeightFor(model string) int {
	if c.ModelWeight == nil {
		return 1
	}
	if w, ok := c.ModelWeight[model]; ok && w > 0 {
		return w
	}
	return 1
}

// EngineCap returns the weight cap for an engine. Returns -1 if uncapped.
func (c Config) EngineCap(engine string) int {
	if c.Concurrency == nil {
		return -1
	}
	cap, ok := c.Concurrency[engine]
	if !ok {
		return -1
	}
	return cap
}

// ResolveProfileEngine returns the actual engine for a profile name.
// Returns the profile's engine if mapped, otherwise falls back to the default engine.
func (c Config) ResolveProfileEngine(profile string) string {
	if c.ProfileEngine != nil {
		if engine, ok := c.ProfileEngine[profile]; ok {
			return engine
		}
	}
	return c.Defaults.Engine
}

// ResolveProfileModel returns the actual model for a profile name.
// Returns the profile's model if mapped, otherwise falls back to the default model.
func (c Config) ResolveProfileModel(profile string) string {
	if c.ProfileModel != nil {
		if model, ok := c.ProfileModel[profile]; ok {
			return model
		}
	}
	return c.Defaults.Model
}

// StallTimeout returns the stall timeout in minutes for a tier.
func (c Config) StallTimeout(tier string) int {
	switch tier {
	case "deep":
		return c.StallTimeoutMin.Deep
	case "heavy":
		return c.StallTimeoutMin.Heavy
	default:
		return c.StallTimeoutMin.Worker
	}
}

// StallTimeoutForTicket returns the stall timeout in minutes, checking
// initiative-level override first, then falling back to the tier default.
func (c Config) StallTimeoutForTicket(initiative, tier string) int {
	if c.Initiatives != nil && initiative != "" {
		if ic, ok := c.Initiatives[initiative]; ok && ic.StallTimeoutMinutes > 0 {
			return ic.StallTimeoutMinutes
		}
	}
	return c.StallTimeout(tier)
}

// GuardianEnabled returns true if guardian config is fully populated.
func (c Config) GuardianEnabled() bool {
	return c.Guardian.Engine != "" && c.Guardian.Model != "" &&
		c.Guardian.Profile != "" && c.Guardian.Initiative != ""
}

func RepoRoot() (string, error) {
	path, err := findConfigFile()
	if err != nil {
		return "", err
	}
	if path != "" {
		return filepath.Dir(path), nil
	}
	return os.Getwd()
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
