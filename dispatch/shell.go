package dispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ShellDispatcher implements Dispatcher by executing agent-mux CLI commands.
type ShellDispatcher struct {
	// BinPath is the path to the agent-mux binary. Defaults to "agent-mux".
	BinPath string

	// SkillPath, when non-empty, is set as AGENT_MUX_SKILL_PATH in the
	// subprocess environment so agent-mux can locate skills even when the
	// parent shell hasn't sourced a login profile.
	SkillPath string
}

// NewShellDispatcher returns a ShellDispatcher with the provided binary path
// and optional skill path.
func NewShellDispatcher(binPath, skillPath string) *ShellDispatcher {
	return &ShellDispatcher{BinPath: binPath, SkillPath: skillPath}
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

	if opts.Engine != "" {
		args = append(args, "--engine", opts.Engine)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--effort", opts.Effort)
	}
	for _, skill := range opts.Skills {
		args = append(args, "--skill", skill)
	}

	return args, contextFile, nil
}

func (s *ShellDispatcher) statusArgs(dispatchID string) []string {
	return []string{"status", dispatchID, "--json"}
}

func (s *ShellDispatcher) runJSON(args []string, workDir string, out any) error {
	cmd := exec.Command(s.binPath(), args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if s.SkillPath != "" {
		cmd.Env = append(os.Environ(), "AGENT_MUX_SKILL_PATH="+s.SkillPath)
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

// Dispatch sends a ticket to agent-mux and parses the dispatch response JSON.
func (s *ShellDispatcher) Dispatch(opts DispatchOptions) (*DispatchResult, error) {
	var result DispatchResult
	args, contextFile, err := s.dispatchArgs(opts)
	if err != nil {
		return nil, err
	}
	if contextFile != "" {
		defer os.Remove(contextFile)
	}
	if err := s.runJSON(args, opts.WorkDir, &result); err != nil {
		return nil, err
	}

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
