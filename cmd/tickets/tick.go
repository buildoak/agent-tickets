package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
)

// stallCheckInterval forces the stall phase to run even when the cards
// directory is unchanged. Stall timeouts are measured in minutes, so a
// pure dir-mtime cursor would let slow dispatches hide behind idle
// periods. 9 minutes is well under the shortest stall timeout (20 min
// for GUARDIAN) so we never miss a transition by more than one cycle.
const stallCheckInterval = 9 * time.Minute

// tickState is the persisted cursor that lets `tick` skip full phase
// execution when nothing has changed since the previous run.
//
// LastCardsMtime + LastStallCheckAt form the "nothing external happened"
// signal. LastOpenReady + LastEngineWeights form the "work is actionable"
// signal. Fast-path must require BOTH to skip — otherwise the tick that
// just dispatched its own work (bumping cards dir mtime) would see the
// fresh mtime equal its own cursor on the next run and skip while the
// queue still has openReady tickets and engines have capacity.
type tickState struct {
	LastCardsMtime    time.Time      `json:"last_cards_mtime"`
	LastStallCheckAt  time.Time      `json:"last_stall_check_at"`
	LastOpenReady     int            `json:"last_open_ready,omitempty"`
	LastEngineWeights map[string]int `json:"last_engine_weights,omitempty"`
}

func cmdTick(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	fs := newFlagSet("tick")
	maxDispatch := fs.Int("max-dispatch", cfg.MaxDispatchPerTick, "max tickets to dispatch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets tick [--max-dispatch N]")
	}

	lockPath := filepath.Join(baseDir, ".tick.lock")
	lock, err := acquireLock(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	defer releaseLock(lock, lockPath)

	// Fast-path: if the cards dir hasn't been modified since last tick AND
	// the stall-check interval hasn't elapsed, skip all phases. Stall
	// detection has its own cadence so slow workers don't hide behind
	// an idle filesystem.
	//
	// "Cards dir unchanged" is necessary but not sufficient: after we
	// dispatch a ticket we bump the dir mtime ourselves (frontmatter
	// write), and we save that fresh mtime as the cursor. The next tick
	// then sees cursor == current mtime even though openReady > 0 and
	// the engine still has capacity to drain more of the queue. To avoid
	// stalling on our own write, also require either an empty ready
	// queue OR all capped engines saturated before taking the fast path.
	state := loadTickState(baseDir)
	now := time.Now()
	cardsMtime, cardsMtimeErr := cardsDirMtime(baseDir)
	dirUnchanged := cardsMtimeErr == nil && !state.LastCardsMtime.IsZero() && cardsMtime.Equal(state.LastCardsMtime)
	stallWindowOpen := now.Sub(state.LastStallCheckAt) >= stallCheckInterval

	if dirUnchanged && !stallWindowOpen {
		if fastPathSafeToSkip(state, cfg) {
			fmt.Fprintln(stdout, "tick: no-change skip")
			return nil
		}
		// Else: cached counts show queued work with available capacity.
		// Fall through and run phases so the queue drains at
		// max_dispatch_per_tick per tick interval.
	}

	docs, err := loadAllTicketDocs(baseDir)
	if err != nil {
		return err
	}

	// Count statuses so we can skip phases with nothing to do.
	var dispatchedCount, openReadyCount int
	for _, td := range docs {
		switch td.Doc.Card.Status {
		case frontmatter.StatusDispatched:
			dispatchedCount++
		case frontmatter.StatusOpen:
			if !td.Doc.Card.Manual {
				openReadyCount++
			}
		}
	}

	reconcileCount := 0
	if dispatchedCount > 0 {
		reconcileCount, err = reconcileTicketsFromDocs(docs, false)
		if err != nil {
			return err
		}
	}

	stallCount := 0
	if dispatchedCount > 0 && (stallWindowOpen || !dirUnchanged) {
		stallCount, err = runStallDetectionFromDocs(docs, cfg)
		if err != nil {
			fmt.Fprintf(stderr, "stall detection error: %v\n", err)
			fmt.Fprintf(stdout, "[STALL_WARNING] stall detection error: %v\n", err)
		}
	}
	// Advance the stall cursor unconditionally when we loaded docs — if
	// there were no dispatched cards the whole question is moot, and we
	// don't want the stall window to stay permanently open blocking the
	// no-change fast path on every subsequent tick.
	state.LastStallCheckAt = now

	dispatchCount := 0
	if openReadyCount > 0 {
		dispatchCount, err = dispatchReadyTicketsFromDocs(baseDir, docs, *maxDispatch, false)
		if err != nil {
			return err
		}
	}

	// Re-stat after phase execution: any writes we did (reconcile status
	// transitions, stall auto-fails, dispatch) bump the dir mtime. If we
	// skipped every phase because nothing needed doing, keep the previous
	// mtime so we don't chase our own tails.
	if updatedMtime, err := cardsDirMtime(baseDir); err == nil {
		state.LastCardsMtime = updatedMtime
	} else if cardsMtimeErr == nil {
		state.LastCardsMtime = cardsMtime
	}

	// Re-scan docs after phase execution so the fast-path cache on the
	// next tick reflects the post-dispatch reality (a ticket we just
	// dispatched moved from open → dispatched and its weight should be
	// counted toward the engine it landed on). Failing the reload is
	// non-fatal — we simply don't update the cache for this tick and
	// the next tick will fall through and re-check the truth.
	if refreshed, refreshErr := loadAllTicketDocs(baseDir); refreshErr == nil {
		state.LastOpenReady = countOpenReady(refreshed)
		state.LastEngineWeights = buildEngineWeightMapFromDocs(refreshed, cfg)
	}

	if err := saveTickState(baseDir, state); err != nil {
		fmt.Fprintf(stderr, "warning: save tick state: %v\n", err)
	}

	fmt.Fprintf(stdout, "tick: reconciled %d, stalled %d, dispatched %d\n", reconcileCount, stallCount, dispatchCount)
	return nil
}

// cardsDirMtime returns the maximum modtime across the `cards/` root and
// all per-initiative subdirectories. Atomic writes (os.Rename) bump the
// parent directory's mtime, so sampling each initiative folder reliably
// detects ticket churn anywhere in the tree without opening any files.
func cardsDirMtime(baseDir string) (time.Time, error) {
	root := filepath.Join(baseDir, "cards")
	info, err := os.Stat(root)
	if err != nil {
		return time.Time{}, err
	}
	latest := info.ModTime()
	entries, err := os.ReadDir(root)
	if err != nil {
		return latest, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subInfo, err := os.Stat(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		if subInfo.ModTime().After(latest) {
			latest = subInfo.ModTime()
		}
	}
	return latest, nil
}

// fastPathSafeToSkip returns true when the cached state from the previous
// tick proves there's nothing actionable right now: either no open-ready
// tickets exist OR every configured engine has already hit its weight cap.
//
// Backward-compat: old .tick-state files written before this field existed
// deserialize with LastEngineWeights == nil. Treat nil as "cache not yet
// populated" and always fall through on the first post-upgrade tick; the
// subsequent tick will have a populated cache and can short-circuit
// correctly. Otherwise we could silently skip real work the first time
// the upgraded binary runs against a pre-existing cursor.
//
// An engine is considered saturated only if (a) it has a cap in
// cfg.Concurrency AND (b) LastEngineWeights[engine] >= cap. Engines
// without a cap are treated as always-available (infinite capacity).
// If any configured engine is not saturated, we assume the queue might
// have work for it and fall through — better a redundant phase run
// than a stalled queue.
func fastPathSafeToSkip(state tickState, cfg config.Config) bool {
	if state.LastEngineWeights == nil {
		return false
	}
	if state.LastOpenReady == 0 {
		return true
	}
	if len(cfg.Concurrency) == 0 {
		// No caps configured anywhere → anything can be dispatched.
		return false
	}
	for engine, cap := range cfg.Concurrency {
		if cap <= 0 {
			continue
		}
		if state.LastEngineWeights[engine] < cap {
			return false
		}
	}
	return true
}

// countOpenReady counts open tickets that are not manual. Matches the
// predicate that dispatch_ready uses to decide which tickets are eligible
// for auto-dispatch (scope-emptiness and dependency readiness are checked
// downstream; for fast-path caching we only need a conservative upper
// bound that shrinks when open tickets complete or become manual).
func countOpenReady(docs []TicketDoc) int {
	n := 0
	for _, td := range docs {
		if td.Doc.Card.Status == frontmatter.StatusOpen && !td.Doc.Card.Manual {
			n++
		}
	}
	return n
}

func tickStatePath(baseDir string) string {
	return filepath.Join(baseDir, ".tick-state")
}

func loadTickState(baseDir string) tickState {
	data, err := os.ReadFile(tickStatePath(baseDir))
	if err != nil {
		return tickState{}
	}
	var state tickState
	if err := json.Unmarshal(data, &state); err != nil {
		return tickState{}
	}
	return state
}

func saveTickState(baseDir string, state tickState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(tickStatePath(baseDir), data, 0o644)
}


func acquireLock(path string) (*os.File, error) {
	for {
		lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			_ = lock.Close()
			if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
				return nil, err
			}
			stale, staleErr := reclaimStaleLock(path)
			if staleErr != nil {
				return nil, staleErr
			}
			if stale {
				continue
			}
			return nil, os.ErrExist
		}
		if err := lock.Truncate(0); err != nil {
			_ = lock.Close()
			return nil, err
		}
		if _, err := lock.Seek(0, 0); err != nil {
			_ = lock.Close()
			return nil, err
		}
		if _, err := fmt.Fprintf(lock, "%d\n", os.Getpid()); err != nil {
			_ = lock.Close()
			return nil, err
		}
		if err := lock.Sync(); err != nil {
			_ = lock.Close()
			return nil, err
		}
		return lock, nil
	}
}

func releaseLock(f *os.File, path string) {
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
	_ = os.Remove(path)
}

func reclaimStaleLock(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, nil
	}
	if processAlive(pid) {
		return false, nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
