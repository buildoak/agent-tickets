package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/buildoak/agent-tickets/config"
)

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

	reconcileCount, err := runReconcile(baseDir)
	if err != nil {
		return err
	}

	stallCount, err := runStallDetection(baseDir, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "stall detection error: %v\n", err)
		fmt.Fprintf(stdout, "[STALL_WARNING] stall detection error: %v\n", err)
	}

	dispatchCount, err := runDispatchReady(baseDir, *maxDispatch)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "tick: reconciled %d, stalled %d, dispatched %d\n", reconcileCount, stallCount, dispatchCount)
	return nil
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
