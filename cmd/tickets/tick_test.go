package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
)

// TestFastPathSafeToSkip locks in the decision table for when the
// fast-path may short-circuit tick's phase execution. This is the
// pure-function core of the 2026-04-21 drain-queue fix.
func TestFastPathSafeToSkip(t *testing.T) {
	caps := config.Config{
		Concurrency: map[string]int{
			"codex":  5,
			"gemini": 4,
			"claude": 3,
		},
	}

	tests := []struct {
		name      string
		state     tickState
		cfg       config.Config
		wantSkip  bool
	}{
		{
			name: "empty queue → skip",
			state: tickState{
				LastOpenReady:     0,
				LastEngineWeights: map[string]int{"codex": 0},
			},
			cfg:      caps,
			wantSkip: true,
		},
		{
			name: "all capped engines saturated → skip",
			state: tickState{
				LastOpenReady: 12,
				LastEngineWeights: map[string]int{
					"codex":  5,
					"gemini": 4,
					"claude": 3,
				},
			},
			cfg:      caps,
			wantSkip: true,
		},
		{
			name: "codex over-cap (race-window overshoot), others capped → skip",
			state: tickState{
				LastOpenReady: 8,
				LastEngineWeights: map[string]int{
					"codex":  6,
					"gemini": 4,
					"claude": 3,
				},
			},
			cfg:      caps,
			wantSkip: true,
		},
		{
			name: "codex has capacity → fall through",
			state: tickState{
				LastOpenReady: 10,
				LastEngineWeights: map[string]int{
					"codex":  3,
					"gemini": 4,
					"claude": 3,
				},
			},
			cfg:      caps,
			wantSkip: false,
		},
		{
			name: "codex at cap but gemini has room → fall through",
			state: tickState{
				LastOpenReady: 10,
				LastEngineWeights: map[string]int{
					"codex":  5,
					"gemini": 0,
					"claude": 3,
				},
			},
			cfg:      caps,
			wantSkip: false,
		},
		{
			name: "codex at cap, missing engines in map treated as zero → fall through",
			state: tickState{
				LastOpenReady: 10,
				LastEngineWeights: map[string]int{
					"codex": 5,
				},
			},
			cfg:      caps,
			wantSkip: false,
		},
		{
			name: "no caps configured → never safe when queue non-empty",
			state: tickState{
				LastOpenReady:     5,
				LastEngineWeights: map[string]int{},
			},
			cfg:      config.Config{},
			wantSkip: false,
		},
		{
			name:     "nil cache (pre-upgrade .tick-state) → always fall through",
			state:    tickState{LastOpenReady: 0, LastEngineWeights: nil},
			cfg:      caps,
			wantSkip: false,
		},
		{
			name: "empty-but-non-nil cache with zero open → skip",
			state: tickState{
				LastOpenReady:     0,
				LastEngineWeights: map[string]int{},
			},
			cfg:      caps,
			wantSkip: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fastPathSafeToSkip(tc.state, tc.cfg)
			if got != tc.wantSkip {
				t.Fatalf("fastPathSafeToSkip = %v, want %v", got, tc.wantSkip)
			}
		})
	}
}

// TestTickStateBackwardCompat confirms .tick-state files written before
// the cache fields existed deserialize cleanly with zero values, and the
// first tick after upgrade falls through (does real work) instead of
// short-circuiting on stale cache semantics.
func TestTickStateBackwardCompat(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)

	// Simulate the old on-disk format: mtime + stall cursor only.
	legacy := map[string]any{
		"last_cards_mtime":     time.Now().Add(-1 * time.Minute).Format(time.RFC3339Nano),
		"last_stall_check_at":  time.Now().Add(-1 * time.Minute).Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, ".tick-state"), raw, 0o644); err != nil {
		t.Fatalf("write legacy .tick-state: %v", err)
	}

	loaded := loadTickState(baseDir)
	if loaded.LastOpenReady != 0 {
		t.Fatalf("legacy .tick-state LastOpenReady = %d, want 0", loaded.LastOpenReady)
	}
	if loaded.LastEngineWeights != nil {
		t.Fatalf("legacy .tick-state LastEngineWeights = %v, want nil", loaded.LastEngineWeights)
	}

	// nil cache must force fall-through even if the other signals would
	// otherwise permit a skip.
	if fastPathSafeToSkip(loaded, config.Config{Concurrency: map[string]int{"codex": 5}}) {
		t.Fatalf("legacy state fast-path skip = true, want false (nil cache must fall through)")
	}
}

// TestTickDrainsQueueAcrossConsecutiveRuns is the integration regression
// guard for the fast-path bug: three consecutive ticks with no external
// filesystem activity must each dispatch one ticket, even though each
// dispatch bumps the cards dir mtime and saves the fresh mtime as the
// cursor. Pre-fix, the second tick would fast-skip.
func TestTickDrainsQueueAcrossConsecutiveRuns(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n\n[concurrency]\ncodex = 5\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")

	// Three ready tickets and a manual one that must NOT count.
	for i := 0; i < 3; i++ {
		mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Ready", "--tier", "worker")
	}
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "Manual", "--tier", "worker", "--manual")

	// Tick 1: dispatches ALPHA-001. mtime bumps from the frontmatter write.
	out1 := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out1, "dispatched 1") {
		t.Fatalf("tick 1 expected dispatched 1:\n%s", out1)
	}

	// Tick 2: same mtime as saved cursor (we wrote it ourselves at end of
	// tick 1). Pre-fix this fast-skipped → 0 dispatched. Post-fix the
	// cached LastOpenReady=2 + codex weight 1 < cap 5 forces fall-through
	// and we dispatch ALPHA-002.
	out2 := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out2, "dispatched 1") {
		t.Fatalf("tick 2 expected dispatched 1 (fast-path must not short-circuit):\n%s", out2)
	}

	// Tick 3: drain the last one.
	out3 := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out3, "dispatched 1") {
		t.Fatalf("tick 3 expected dispatched 1:\n%s", out3)
	}

	// Tick 4: no more ready tickets (manual one doesn't count). Cache
	// LastOpenReady=0 → fast-path skip is legitimate now.
	out4 := mustRunStdout(t, "tick", "--max-dispatch", "1")
	if !strings.Contains(out4, "no-change skip") && !strings.Contains(out4, "dispatched 0") {
		t.Fatalf("tick 4 expected no-change skip or dispatched 0:\n%s", out4)
	}
}

// TestTickStatePersistsCounts confirms that after a tick runs, the
// .tick-state file holds the freshly-computed LastOpenReady and
// LastEngineWeights so the next tick can make a correct fast-path
// decision without re-scanning the tree.
func TestTickStatePersistsCounts(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("TICKETS_BASE_DIR", baseDir)
	writeConfigFile(t, baseDir, "[defaults]\nengine = \"codex\"\nmodel = \"gpt-5.4-mini\"\n\n[concurrency]\ncodex = 5\n")
	withWorkingDir(t, baseDir)
	withMockDispatcher(t, &dispatch.MockDispatcher{})

	mustRun(t, "init", "ALPHA", "--title", "Alpha")
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "A", "--tier", "worker")
	mustRunStdout(t, "create", "--initiative", "ALPHA", "--title", "B", "--tier", "worker")

	mustRunStdout(t, "tick", "--max-dispatch", "1")

	state := loadTickState(baseDir)
	if state.LastOpenReady != 1 {
		t.Fatalf("LastOpenReady after 1 dispatch = %d, want 1", state.LastOpenReady)
	}
	if state.LastEngineWeights == nil {
		t.Fatalf("LastEngineWeights nil after tick, want populated map")
	}
	// Default codex + gpt-5.4-mini → weight 1 (ModelWeightFor returns 1
	// when no model_weight map configured). Assert bucket >= 1.
	if state.LastEngineWeights["codex"] < 1 {
		t.Fatalf("LastEngineWeights[codex] = %d, want >= 1 (ticket just dispatched)",
			state.LastEngineWeights["codex"])
	}
}

// TestCountOpenReadyExcludesManual mirrors the dispatch-ready filter so
// the fast-path cache doesn't over-count tickets that will never be
// auto-dispatched.
func TestCountOpenReadyExcludesManual(t *testing.T) {
	docs := []TicketDoc{
		{Doc: &frontmatter.Document{Card: frontmatter.Card{
			Status: frontmatter.StatusOpen, Manual: false,
		}}},
		{Doc: &frontmatter.Document{Card: frontmatter.Card{
			Status: frontmatter.StatusOpen, Manual: true,
		}}},
		{Doc: &frontmatter.Document{Card: frontmatter.Card{
			Status: frontmatter.StatusDispatched, Manual: false,
		}}},
		{Doc: &frontmatter.Document{Card: frontmatter.Card{
			Status: frontmatter.StatusDone, Manual: false,
		}}},
		{Doc: &frontmatter.Document{Card: frontmatter.Card{
			Status: frontmatter.StatusOpen, Manual: false,
		}}},
	}
	if got := countOpenReady(docs); got != 2 {
		t.Fatalf("countOpenReady = %d, want 2 (open && !manual)", got)
	}
}
