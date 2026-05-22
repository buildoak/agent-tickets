package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	root := t.TempDir()
	// Resolve symlinks so the expected path matches what filepath.Dir returns
	// after os.Stat resolves symlinks on macOS (/var -> /private/var).
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	writeConfig(t, root, `
base_dir = "./tickets"
agent_mux_bin = "custom-agent-mux"
max_retry = 5

[defaults]
engine = "codex"
model = "gpt-5.4"
effort = "high"
profile = "prod"
`)

	child := filepath.Join(root, "nested", "deeper")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}

	prev, restore := chdir(t, child)
	defer restore(prev)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	wantBaseDir := filepath.Join(rootReal, "tickets")
	if cfg.BaseDir != wantBaseDir {
		t.Fatalf("base dir mismatch: %q", cfg.BaseDir)
	}
	if cfg.AgentMuxBin != "custom-agent-mux" {
		t.Fatalf("agent mux bin mismatch: %q", cfg.AgentMuxBin)
	}
	if cfg.MaxRetry != 5 {
		t.Fatalf("max retry mismatch: %d", cfg.MaxRetry)
	}
	if cfg.Defaults.Model != "gpt-5.4" {
		t.Fatalf("defaults model mismatch: %q", cfg.Defaults.Model)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
base_dir = "./tickets"
agent_mux_bin = "custom-agent-mux"
`)

	prev, restore := chdir(t, root)
	defer restore(prev)

	t.Setenv("TICKETS_BASE_DIR", "/tmp/override")
	t.Setenv("TICKETS_AGENT_MUX_BIN", "env-agent-mux")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.BaseDir != "/tmp/override" {
		t.Fatalf("base dir mismatch: %q", cfg.BaseDir)
	}
	if cfg.AgentMuxBin != "env-agent-mux" {
		t.Fatalf("agent mux bin mismatch: %q", cfg.AgentMuxBin)
	}
}

func TestLoadDefaultsWhenMissingFile(t *testing.T) {
	root := t.TempDir()
	prev, restore := chdir(t, root)
	defer restore(prev)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.BaseDir != defaultBaseDir {
		t.Fatalf("expected default base dir %q, got %q", defaultBaseDir, cfg.BaseDir)
	}
	if cfg.AgentMuxBin != defaultAgentMuxBin {
		t.Fatalf("agent mux bin mismatch: %q", cfg.AgentMuxBin)
	}
	if cfg.MaxRetry != defaultMaxRetry {
		t.Fatalf("max retry mismatch: %d", cfg.MaxRetry)
	}
	if cfg.Defaults.Profile != defaultProfile {
		t.Fatalf("profile mismatch: %q", cfg.Defaults.Profile)
	}
}

func TestLoadPartialFileUsesDefaults(t *testing.T) {
	root := t.TempDir()
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	writeConfig(t, root, `
base_dir = "./tickets"

[defaults]
engine = "codex"
`)

	prev, restore := chdir(t, root)
	defer restore(prev)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	wantBaseDir := filepath.Join(rootReal, "tickets")
	if cfg.BaseDir != wantBaseDir {
		t.Fatalf("base dir mismatch: %q", cfg.BaseDir)
	}
	if cfg.AgentMuxBin != defaultAgentMuxBin {
		t.Fatalf("agent mux bin mismatch: %q", cfg.AgentMuxBin)
	}
	if cfg.MaxRetry != defaultMaxRetry {
		t.Fatalf("max retry mismatch: %d", cfg.MaxRetry)
	}
	if cfg.Defaults.Engine != "codex" {
		t.Fatalf("defaults engine mismatch: %q", cfg.Defaults.Engine)
	}
}

func TestLoadParsesSchedulerFields(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
max_dispatch_per_tick = 2
skill_path = "/tmp/skills"

[defaults]
engine = "codex"
model = "gpt-5.4-mini"
profile = "ticket-worker"

[concurrency]
codex = 5
gemini = 4

[model_weight]
"gpt-5.4-mini" = 1
"gpt-5.4" = 2

[stall_timeout_minutes]
worker = 11
deep = 22
heavy = 33

[guardian]
engine = "codex"
model = "gpt-5.4-mini"
effort = "xhigh"
profile = "ticket-worker"
initiative = "AUDIT"
`)

	prev, restore := chdir(t, root)
	defer restore(prev)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.MaxDispatchPerTick != 2 {
		t.Fatalf("max dispatch per tick mismatch: %d", cfg.MaxDispatchPerTick)
	}
	if cfg.SkillPath != "/tmp/skills" {
		t.Fatalf("skill path mismatch: %q", cfg.SkillPath)
	}
	if got := cfg.EngineCap("codex"); got != 5 {
		t.Fatalf("engine cap mismatch: %d", got)
	}
	if got := cfg.EngineCap("unknown"); got != -1 {
		t.Fatalf("expected uncapped engine, got %d", got)
	}
	if got := cfg.ModelWeightFor("gpt-5.4"); got != 2 {
		t.Fatalf("model weight mismatch: %d", got)
	}
	if got := cfg.ModelWeightFor("missing"); got != 1 {
		t.Fatalf("default model weight mismatch: %d", got)
	}
	if got := cfg.StallTimeout("worker"); got != 11 {
		t.Fatalf("worker stall timeout mismatch: %d", got)
	}
	if got := cfg.StallTimeout("deep"); got != 22 {
		t.Fatalf("deep stall timeout mismatch: %d", got)
	}
	if got := cfg.StallTimeout("heavy"); got != 33 {
		t.Fatalf("heavy stall timeout mismatch: %d", got)
	}
	if !cfg.GuardianEnabled() {
		t.Fatalf("expected guardian config enabled")
	}
}

func TestLoadAppliesSchedulerDefaults(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
[defaults]
engine = "codex"
`)

	prev, restore := chdir(t, root)
	defer restore(prev)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.MaxDispatchPerTick != 1 {
		t.Fatalf("max dispatch per tick default mismatch: %d", cfg.MaxDispatchPerTick)
	}
	if cfg.StallTimeoutMin.Worker != 30 || cfg.StallTimeoutMin.Deep != 45 || cfg.StallTimeoutMin.Heavy != 60 {
		t.Fatalf("unexpected stall defaults: %+v", cfg.StallTimeoutMin)
	}
	if cfg.GuardianEnabled() {
		t.Fatalf("guardian should be disabled without full config")
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, ".tickets.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func chdir(t *testing.T, dir string) (string, func(string)) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	return prev, func(path string) {
		t.Helper()
		if err := os.Chdir(path); err != nil {
			t.Fatalf("restore dir %s: %v", path, err)
		}
	}
}
