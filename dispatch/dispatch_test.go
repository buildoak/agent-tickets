package dispatch

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMockDispatcherDefaults(t *testing.T) {
	t.Parallel()

	dispatcher := &MockDispatcher{}

	dispatchResult, err := dispatcher.Dispatch(DispatchOptions{})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}

	if want := (&DispatchResult{DispatchID: "mock-dispatch-id", SessionID: "mock-session-id"}); !reflect.DeepEqual(dispatchResult, want) {
		t.Fatalf("Dispatch() = %#v, want %#v", dispatchResult, want)
	}

	statusResult, err := dispatcher.Status("ignored")
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	if want := (&StatusResult{Status: "completed"}); !reflect.DeepEqual(statusResult, want) {
		t.Fatalf("Status() = %#v, want %#v", statusResult, want)
	}
}

func TestMockDispatcherCustomDispatchFunc(t *testing.T) {
	t.Parallel()

	opts := DispatchOptions{
		Profile:    "retry",
		Engine:     "codex",
		Model:      "gpt-5",
		Effort:     "high",
		WorkDir:    "/tmp",
		TicketPath: "/tmp/ticket.md",
		Preamble:   "retry context",
	}

	called := false
	dispatcher := &MockDispatcher{
		DispatchFunc: func(got DispatchOptions) (*DispatchResult, error) {
			called = true
			if !reflect.DeepEqual(got, opts) {
				t.Fatalf("Dispatch options = %#v, want %#v", got, opts)
			}

			return &DispatchResult{DispatchID: "custom-dispatch", SessionID: "custom-session"}, nil
		},
	}

	result, err := dispatcher.Dispatch(opts)
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !called {
		t.Fatal("DispatchFunc was not called")
	}

	if want := (&DispatchResult{DispatchID: "custom-dispatch", SessionID: "custom-session"}); !reflect.DeepEqual(result, want) {
		t.Fatalf("Dispatch() = %#v, want %#v", result, want)
	}
}

func TestMockDispatcherCustomStatusFunc(t *testing.T) {
	t.Parallel()

	const dispatchID = "dispatch-123"
	called := false
	dispatcher := &MockDispatcher{
		StatusFunc: func(got string) (*StatusResult, error) {
			called = true
			if got != dispatchID {
				t.Fatalf("Status dispatchID = %q, want %q", got, dispatchID)
			}

			return &StatusResult{
				Status: "running",
				Tokens: &TokenData{In: 10, Out: 5, Cache: 3, PeakContext: 42},
			}, nil
		},
	}

	result, err := dispatcher.Status(dispatchID)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !called {
		t.Fatal("StatusFunc was not called")
	}

	if want := (&StatusResult{
		Status: "running",
		Tokens: &TokenData{In: 10, Out: 5, Cache: 3, PeakContext: 42},
	}); !reflect.DeepEqual(result, want) {
		t.Fatalf("Status() = %#v, want %#v", result, want)
	}
}

func TestShellDispatcherDispatchBuildsArgs(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("", "")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:    "default",
		Engine:     "codex",
		Model:      "gpt-5.4",
		Effort:     "medium",
		WorkDir:    "/tmp/worktree",
		TicketPath: "/tmp/ticket.md",
		Preamble:   "retry context",
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile == "" {
		t.Fatal("dispatchArgs() did not create a context file")
	}
	defer os.Remove(contextFile)

	want := []string{
		"dispatch",
		"--async",
		"--profile", "default",
		"--prompt-file", "/tmp/ticket.md",
		"--cwd", "/tmp/worktree",
		"--context-file", contextFile,
		"--engine", "codex",
		"--model", "gpt-5.4",
		"--effort", "medium",
	}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("dispatchArgs() = %#v, want %#v", args, want)
	}
	data, readErr := os.ReadFile(contextFile)
	if readErr != nil {
		t.Fatalf("read context file: %v", readErr)
	}
	if string(data) != "retry context" {
		t.Fatalf("context file contents = %q, want %q", string(data), "retry context")
	}
}

func TestShellDispatcherStatusBuildsArgs(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("", "")

	args := dispatcher.statusArgs("dispatch-123")
	want := []string{"status", "dispatch-123", "--json"}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("statusArgs() = %#v, want %#v", args, want)
	}
}

func TestShellDispatcherDispatchBuildsArgsWithSkills(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("", "")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:    "default",
		Engine:     "codex",
		Model:      "gpt-5.4",
		TicketPath: "/tmp/ticket.md",
		Skills:     []string{"web-search", "code-review"},
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile != "" {
		defer os.Remove(contextFile)
	}

	want := []string{
		"dispatch",
		"--async",
		"--profile", "default",
		"--prompt-file", "/tmp/ticket.md",
		"--engine", "codex",
		"--model", "gpt-5.4",
		"--skill", "web-search",
		"--skill", "code-review",
	}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("dispatchArgs() = %#v, want %#v", args, want)
	}
}

func TestShellDispatcherDispatchOmitsCwdWhenEmpty(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("", "")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:    "default",
		Engine:     "codex",
		Model:      "gpt-5.4",
		TicketPath: "/tmp/ticket.md",
		WorkDir:    "",
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile != "" {
		defer os.Remove(contextFile)
	}

	for i, arg := range args {
		if arg == "--cwd" {
			t.Fatalf("dispatchArgs() should not contain --cwd when WorkDir is empty, found at index %d: %#v", i, args)
		}
	}
}

func TestShellDispatcherDispatchPassesCwd(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("", "")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:    "default",
		Engine:     "codex",
		Model:      "gpt-5.4",
		TicketPath: "/tmp/ticket.md",
		WorkDir:    "/home/user/project",
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile != "" {
		defer os.Remove(contextFile)
	}

	found := false
	for i, arg := range args {
		if arg == "--cwd" && i+1 < len(args) && args[i+1] == "/home/user/project" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("dispatchArgs() should contain --cwd /home/user/project: %#v", args)
	}
}

func TestShellDispatcherRunJSONIncludesStderr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := filepath.Join(dir, "agent-mux.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'backend exploded\\n' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	dispatcher := NewShellDispatcher(script, "")
	err := dispatcher.runJSON([]string{"status", "dispatch-123", "--json"}, dir, &StatusResult{})
	if err == nil {
		t.Fatal("runJSON() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("runJSON() error = %q, want stderr text", err)
	}
}

func TestShellDispatcherSkillPathInjectsEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Script prints AGENT_MUX_SKILL_PATH from its environment as JSON so
	// runJSON can parse it. This proves the env var reached the subprocess.
	script := filepath.Join(dir, "agent-mux.sh")
	if err := os.WriteFile(script, []byte(
		"#!/bin/sh\nprintf '{\"skill_path\":\"%s\"}' \"$AGENT_MUX_SKILL_PATH\"\n",
	), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	const wantPath = "/a/skills:/b/skills"
	dispatcher := NewShellDispatcher(script, wantPath)

	var result struct {
		SkillPath string `json:"skill_path"`
	}
	if err := dispatcher.runJSON([]string{}, "", &result); err != nil {
		t.Fatalf("runJSON() error = %v", err)
	}
	if result.SkillPath != wantPath {
		t.Fatalf("AGENT_MUX_SKILL_PATH = %q, want %q", result.SkillPath, wantPath)
	}
}

func TestShellDispatcherNoSkillPathLeavesEnvNil(t *testing.T) {
	t.Parallel()

	// When SkillPath is empty, cmd.Env should remain nil (inherit parent env).
	// We verify by checking that the script sees the parent's env unchanged.
	dir := t.TempDir()
	script := filepath.Join(dir, "agent-mux.sh")
	if err := os.WriteFile(script, []byte(
		"#!/bin/sh\nprintf '{\"home\":\"%s\"}' \"$HOME\"\n",
	), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	dispatcher := NewShellDispatcher(script, "")

	var result struct {
		Home string `json:"home"`
	}
	if err := dispatcher.runJSON([]string{}, "", &result); err != nil {
		t.Fatalf("runJSON() error = %v", err)
	}
	if result.Home == "" {
		t.Fatal("subprocess did not inherit parent HOME; cmd.Env may have been incorrectly set")
	}
}
