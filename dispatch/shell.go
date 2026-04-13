package dispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// ShellDispatcher implements Dispatcher by executing agent-mux CLI commands.
type ShellDispatcher struct {
	// BinPath is the path to the agent-mux binary. Defaults to "agent-mux".
	BinPath string
}

// NewShellDispatcher returns a ShellDispatcher with the provided binary path.
func NewShellDispatcher(binPath string) *ShellDispatcher {
	return &ShellDispatcher{BinPath: binPath}
}

func (s *ShellDispatcher) binPath() string {
	if s.BinPath == "" {
		return "agent-mux"
	}

	return s.BinPath
}

func (s *ShellDispatcher) dispatchArgs(opts DispatchOptions) ([]string, string, error) {
	args := []string{
		"dispatch",
		"--async",
		"--profile", opts.Profile,
		"--prompt-file", opts.TicketPath,
	}
	if opts.WorkDir != "" {
		args = append(args, "--cwd", opts.WorkDir)
	}
	contextFile := ""
	if strings.TrimSpace(opts.Preamble) != "" {
		file, err := os.CreateTemp("", "agent-tickets-context-*.txt")
		if err != nil {
			return nil, "", fmt.Errorf("create dispatch context file: %w", err)
		}
		if _, err := file.WriteString(opts.Preamble); err != nil {
			_ = file.Close()
			_ = os.Remove(file.Name())
			return nil, "", fmt.Errorf("write dispatch context file: %w", err)
		}
		if err := file.Close(); err != nil {
			_ = os.Remove(file.Name())
			return nil, "", fmt.Errorf("close dispatch context file: %w", err)
		}
		contextFile = file.Name()
		args = append(args, "--context-file", contextFile)
	}

	// Only pass engine/model/effort when they were explicitly set at CLI or
	// card level. When they fall through to config defaults and a profile is
	// set from a higher-priority source (initiative or card), omit them so
	// agent-mux lets the profile define the engine/model/effort.
	passEngine := opts.Engine != "" && ShouldPassEngineFlags(opts)
	if passEngine {
		args = append(args, "--engine", opts.Engine)
	}
	if opts.Model != "" && passEngine {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" && passEngine {
		args = append(args, "--effort", opts.Effort)
	}
	for _, skill := range opts.Skills {
		args = append(args, "--skill", skill)
	}

	return args, contextFile, nil
}

// ShouldPassEngineFlags returns true when engine/model/effort flags should be
// forwarded to agent-mux. The rule: if engine/model/effort come from config
// defaults (lowest priority) and a profile is set from a higher-priority source
// (CLI, card, or initiative), omit them — let the profile define the engine.
// When sources are unset (SourceNone / zero value), we pass the flags through
// for backward compatibility (callers that don't populate sources).
func ShouldPassEngineFlags(opts DispatchOptions) bool {
	engineSrc := opts.EngineSource
	profileSrc := opts.ProfileSource

	// No source tracking → backward compatible: always pass.
	if engineSrc == "" || engineSrc == SourceNone {
		return true
	}

	// Engine explicitly set at CLI or card level → always pass.
	if engineSrc == SourceCLI || engineSrc == SourceCard {
		return true
	}

	// Engine from config defaults. If profile comes from a higher source
	// (CLI, card, or initiative), let the profile handle it.
	if engineSrc == SourceConfig {
		if profileSrc == SourceCLI || profileSrc == SourceCard || profileSrc == SourceInitiative {
			return false
		}
	}

	return true
}

func (s *ShellDispatcher) statusArgs(dispatchID string) []string {
	return []string{"status", dispatchID, "--json"}
}

func (s *ShellDispatcher) runJSON(args []string, workDir string, out any) error {
	cmd := exec.Command(s.binPath(), args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return fmt.Errorf("run %q: %w: %s", s.binPath(), err, stderr)
			}
		}
		return fmt.Errorf("run %q: %w", s.binPath(), err)
	}

	if err := json.Unmarshal(output, out); err != nil {
		return fmt.Errorf("parse %q output: %w", s.binPath(), err)
	}

	return nil
}

// Dispatch sends a ticket to agent-mux --async and reads the initial
// async_started JSON response from stdout without waiting for the worker
// to finish. The agent-mux process continues running in the background.
func (s *ShellDispatcher) Dispatch(opts DispatchOptions) (*DispatchResult, error) {
	args, contextFile, err := s.dispatchArgs(opts)
	if err != nil {
		return nil, err
	}
	// contextFile is a temp preamble file; clean it up after the process
	// has started and read the initial JSON (the file has been consumed by
	// agent-mux by then).
	if contextFile != "" {
		defer os.Remove(contextFile)
	}

	cmd := exec.Command(s.binPath(), args...)
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	// Put agent-mux in its own process group so it isn't killed by
	// group signals when the parent (tick) process exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe for %q: %w", s.binPath(), err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", s.binPath(), err)
	}

	// Read the first JSON object from stdout. agent-mux --async emits this
	// immediately on dispatch:
	//   {"schema_version":1,"kind":"async_started","dispatch_id":"...","artifact_dir":"..."}
	var result DispatchResult
	dec := json.NewDecoder(stdoutPipe)
	if err := dec.Decode(&result); err != nil {
		// If JSON decode fails, try to reap the process to get stderr.
		_ = cmd.Wait()
		return nil, fmt.Errorf("parse %q async response: %w", s.binPath(), err)
	}

	// We have the dispatch_id. Drain remaining stdout in the background
	// so agent-mux doesn't get SIGPIPE when writing subsequent responses
	// (e.g., the async_started JSON). The goroutine also reaps the process.
	go func() {
		_, _ = io.Copy(io.Discard, stdoutPipe)
		_ = cmd.Wait()
	}()

	return &result, nil
}

// Status queries agent-mux for a dispatch status and parses the response JSON.
func (s *ShellDispatcher) Status(dispatchID string) (*StatusResult, error) {
	var result StatusResult
	if err := s.runJSON(s.statusArgs(dispatchID), "", &result); err != nil {
		return nil, err
	}

	return &result, nil
}
