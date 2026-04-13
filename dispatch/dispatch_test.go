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

	if want := (&DispatchResult{DispatchID: "mock-dispatch-id"}); !reflect.DeepEqual(dispatchResult, want) {
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
		Skills:     []string{"ticket-work", "web-search"},
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

	dispatcher := NewShellDispatcher("")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:    "default",
		Engine:     "codex",
		Model:      "gpt-5.4",
		Effort:     "medium",
		WorkDir:    "/tmp/worktree",
		Skills:     []string{"ticket-work", "web-search"},
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
		"--skill", "ticket-work",
		"--skill", "web-search",
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

func TestShouldPassEngineFlagsBackwardCompat(t *testing.T) {
	t.Parallel()

	// No source tracking (zero values) → always pass engine flags.
	if !ShouldPassEngineFlags(DispatchOptions{Engine: "codex"}) {
		t.Fatal("expected engine flags to pass with no source tracking")
	}
}

func TestShouldPassEngineFlagsCLIEngine(t *testing.T) {
	t.Parallel()

	// Engine explicitly set from CLI → always pass.
	if !ShouldPassEngineFlags(DispatchOptions{
		Engine:        "claude",
		EngineSource:  SourceCLI,
		ProfileSource: SourceInitiative,
	}) {
		t.Fatal("expected engine flags to pass when engine from CLI")
	}
}

func TestShouldPassEngineFlagsCardEngine(t *testing.T) {
	t.Parallel()

	// Engine from card frontmatter → always pass.
	if !ShouldPassEngineFlags(DispatchOptions{
		Engine:        "gemini",
		EngineSource:  SourceCard,
		ProfileSource: SourceInitiative,
	}) {
		t.Fatal("expected engine flags to pass when engine from card")
	}
}

func TestShouldPassEngineFlagsConfigEngineWithInitProfile(t *testing.T) {
	t.Parallel()

	// Engine from config defaults, profile from initiative → omit engine flags.
	if ShouldPassEngineFlags(DispatchOptions{
		Engine:        "codex",
		EngineSource:  SourceConfig,
		ProfileSource: SourceInitiative,
	}) {
		t.Fatal("expected engine flags to be omitted when engine from config and profile from initiative")
	}
}

func TestShouldPassEngineFlagsConfigEngineWithCardProfile(t *testing.T) {
	t.Parallel()

	// Engine from config defaults, profile from card → omit engine flags.
	if ShouldPassEngineFlags(DispatchOptions{
		Engine:        "codex",
		EngineSource:  SourceConfig,
		ProfileSource: SourceCard,
	}) {
		t.Fatal("expected engine flags to be omitted when engine from config and profile from card")
	}
}

func TestShouldPassEngineFlagsConfigEngineWithConfigProfile(t *testing.T) {
	t.Parallel()

	// Both engine and profile from config → pass engine flags.
	if !ShouldPassEngineFlags(DispatchOptions{
		Engine:        "codex",
		EngineSource:  SourceConfig,
		ProfileSource: SourceConfig,
	}) {
		t.Fatal("expected engine flags to pass when both from config")
	}
}

func TestDispatchArgsOmitsEngineFlagsWhenProfileFromInitiative(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:       "paper-ops-worker",
		Engine:        "codex",
		Model:         "gpt-5.4-mini",
		Effort:        "xhigh",
		WorkDir:       "/tmp/worktree",
		TicketPath:    "/tmp/ticket.md",
		ProfileSource: SourceInitiative,
		EngineSource:  SourceConfig,
		ModelSource:   SourceConfig,
		EffortSource:  SourceConfig,
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile != "" {
		os.Remove(contextFile)
	}

	// Engine/model/effort flags should NOT appear.
	for _, flag := range []string{"--engine", "--model", "--effort"} {
		for _, arg := range args {
			if arg == flag {
				t.Fatalf("dispatchArgs() should not contain %s when engine from config and profile from initiative, got: %v", flag, args)
			}
		}
	}

	// Profile flag should still appear.
	foundProfile := false
	for i, arg := range args {
		if arg == "--profile" && i+1 < len(args) && args[i+1] == "paper-ops-worker" {
			foundProfile = true
			break
		}
	}
	if !foundProfile {
		t.Fatalf("dispatchArgs() missing --profile paper-ops-worker, got: %v", args)
	}
}

func TestDispatchArgsPassesEngineFlagsWhenFromCLI(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("")

	args, contextFile, err := dispatcher.dispatchArgs(DispatchOptions{
		Profile:       "paper-ops-worker",
		Engine:        "claude",
		Model:         "opus",
		Effort:        "high",
		WorkDir:       "/tmp/worktree",
		TicketPath:    "/tmp/ticket.md",
		ProfileSource: SourceInitiative,
		EngineSource:  SourceCLI,
		ModelSource:   SourceCLI,
		EffortSource:  SourceCLI,
	})
	if err != nil {
		t.Fatalf("dispatchArgs() error = %v", err)
	}
	if contextFile != "" {
		os.Remove(contextFile)
	}

	// Engine/model/effort flags SHOULD appear when from CLI.
	want := map[string]string{"--engine": "claude", "--model": "opus", "--effort": "high"}
	for flag, val := range want {
		found := false
		for i, arg := range args {
			if arg == flag && i+1 < len(args) && args[i+1] == val {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("dispatchArgs() missing %s %s, got: %v", flag, val, args)
		}
	}
}

func TestShellDispatcherStatusBuildsArgs(t *testing.T) {
	t.Parallel()

	dispatcher := NewShellDispatcher("")

	args := dispatcher.statusArgs("dispatch-123")
	want := []string{"status", "dispatch-123", "--json"}

	if !reflect.DeepEqual(args, want) {
		t.Fatalf("statusArgs() = %#v, want %#v", args, want)
	}
}

func TestShellDispatcherRunJSONIncludesStderr(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := filepath.Join(dir, "agent-mux.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'backend exploded\\n' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	dispatcher := NewShellDispatcher(script)
	err := dispatcher.runJSON([]string{"status", "dispatch-123", "--json"}, dir, &StatusResult{})
	if err == nil {
		t.Fatal("runJSON() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "backend exploded") {
		t.Fatalf("runJSON() error = %q, want stderr text", err)
	}
}
