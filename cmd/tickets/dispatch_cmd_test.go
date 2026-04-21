package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
)

// TestResolveDispatchStagger locks in the stagger-decision contract.
// The actual sleep is issued by cmdDispatch; this isolates the pure
// decision logic so the behavior is fast and deterministic to test.
func TestResolveDispatchStagger(t *testing.T) {
	tests := []struct {
		name     string
		idsCount int
		cfg      int
		flag     int
		want     int
	}{
		// Solo dispatch: NEVER sleep, regardless of anything else.
		{"solo-default", 1, 0, -1, 0},
		{"solo-cfg-high", 1, 30, -1, 0},
		{"solo-flag-explicit", 1, 0, 30, 0},
		{"zero-ids", 0, 15, -1, 0},

		// Multi-ID, flag unset: apply 15s floor over cfg.
		{"multi-cfg-zero-floor-applies", 3, 0, -1, 15},
		{"multi-cfg-below-floor", 2, 10, -1, 15},
		{"multi-cfg-at-floor", 2, 15, -1, 15},
		{"multi-cfg-above-floor", 4, 30, -1, 30},

		// Multi-ID, flag explicit: flag wins, no floor.
		{"multi-flag-zero-disables", 6, 15, 0, 0},
		{"multi-flag-below-floor-wins", 3, 15, 5, 5},
		{"multi-flag-above-cfg", 3, 10, 45, 45},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := resolveDispatchStagger(tc.idsCount, tc.cfg, tc.flag)
			if got != tc.want {
				t.Fatalf("resolveDispatchStagger(ids=%d,cfg=%d,flag=%d) = %d, want %d",
					tc.idsCount, tc.cfg, tc.flag, got, tc.want)
			}
		})
	}
}

// TestResolveDispatchStaggerSourceLabels confirms the human-readable source
// string includes enough context for users to reason about the value.
func TestResolveDispatchStaggerSourceLabels(t *testing.T) {
	cases := []struct {
		name         string
		idsCount     int
		cfg          int
		flag         int
		wantContains string
	}{
		{"floor-source", 2, 0, -1, "floor"},
		{"config-source", 2, 30, -1, "stagger_seconds"},
		{"flag-source", 2, 0, 45, "--stagger-seconds"},
		{"flag-disabled", 2, 15, 0, "disabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, src := resolveDispatchStagger(tc.idsCount, tc.cfg, tc.flag)
			if !strings.Contains(src, tc.wantContains) {
				t.Fatalf("source %q does not contain %q", src, tc.wantContains)
			}
		})
	}
}

// TestDispatchSoloNoSleep confirms solo dispatch does not introduce any
// observable delay. This is the regression guard — solo behavior must be
// identical to pre-fix (no stagger ever).
func TestDispatchSoloNoSleep(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	id := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Solo", "--tier", "worker"))

	start := time.Now()
	mustRun(t, "dispatch", id, "--engine", "codex", "--model", "gpt-5.4")
	elapsed := time.Since(start)

	// Solo dispatch must complete well under the multi-ID stagger floor.
	if elapsed > 5*time.Second {
		t.Fatalf("solo dispatch took %v, expected < 5s (no sleep should fire for len(ids)==1)", elapsed)
	}

	doc := mustParseTicket(t, baseDir, id)
	if doc.Card.Status != frontmatter.StatusDispatched {
		t.Fatalf("solo dispatch status = %s, want dispatched", doc.Card.Status)
	}
}

// TestDispatchMultiIDFlagZeroDisablesSleep confirms --stagger-seconds=0
// short-circuits the sleep for advanced users who know the race risk.
func TestDispatchMultiIDFlagZeroDisablesSleep(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker"))
	c := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C", "--tier", "worker"))

	start := time.Now()
	mustRun(t, "dispatch", a+","+b+","+c,
		"--engine", "codex", "--model", "gpt-5.4",
		"--stagger-seconds", "0")
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Fatalf("multi-ID dispatch with --stagger-seconds=0 took %v, expected no sleep", elapsed)
	}

	for _, id := range []string{a, b, c} {
		doc := mustParseTicket(t, baseDir, id)
		if doc.Card.Status != frontmatter.StatusDispatched {
			t.Fatalf("%s status = %s, want dispatched", id, doc.Card.Status)
		}
	}
}

// TestDispatchMultiIDAnnounceStagger confirms the multi-ID stagger banner
// is printed to stdout so operators see what's happening. We use
// --stagger-seconds=0 to avoid an actual sleep but still expect no banner
// in that case. Then we run a separate case with a 1s stagger and verify
// the banner appears (1s keeps the test fast).
func TestDispatchMultiIDAnnounceStagger(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	a := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker"))
	b := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker"))

	// Case 1: --stagger-seconds=0 → no banner, instant.
	bufZero := stdout.(*bytes.Buffer)
	bufZero.Reset()
	mustRun(t, "dispatch", a+","+b,
		"--engine", "codex", "--model", "gpt-5.4",
		"--stagger-seconds", "0")
	if strings.Contains(bufZero.String(), "applying") {
		t.Fatalf("--stagger-seconds=0 should not print stagger banner:\n%s", bufZero.String())
	}

	// Reset cards to open for a second dispatch cycle.
	// Instead, just create two fresh tickets for case 2.
	c := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "C", "--tier", "worker"))
	d := strings.TrimSpace(mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "D", "--tier", "worker"))

	// Case 2: explicit --stagger-seconds=1 → banner visible, ~1s sleep.
	buf := stdout.(*bytes.Buffer)
	buf.Reset()
	start := time.Now()
	mustRun(t, "dispatch", c+","+d,
		"--engine", "codex", "--model", "gpt-5.4",
		"--stagger-seconds", "1")
	elapsed := time.Since(start)
	if !strings.Contains(buf.String(), "applying 1s stagger") {
		t.Fatalf("expected stagger banner 'applying 1s stagger', got:\n%s", buf.String())
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected ~1s sleep between dispatches, got %v", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected ~1s sleep, got %v (unexpected delay)", elapsed)
	}
}
