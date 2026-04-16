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
type tickState struct {
	LastCardsMtime    time.Time `json:"last_cards_mtime"`
	LastStallCheckAt  time.Time `json:"last_stall_check_at"`
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
	state := loadTickState(baseDir)
	now := time.Now()
	cardsMtime, cardsMtimeErr := cardsDirMtime(baseDir)
	dirUnchanged := cardsMtimeErr == nil && !state.LastCardsMtime.IsZero() && cardsMtime.Equal(state.LastCardsMtime)
	stallWindowOpen := now.Sub(state.LastStallCheckAt) >= stallCheckInterval

	if dirUnchanged && !stallWindowOpen {
		fmt.Fprintln(stdout, "tick: no-change skip")
		return nil
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
