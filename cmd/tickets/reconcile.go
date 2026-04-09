package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

func cmdReconcile(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("reconcile")
	dryRun := fs.Bool("dry-run", false, "preview reconcile changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets reconcile [--dry-run]")
	}

	_, err = reconcileTickets(baseDir, *dryRun)
	return err
}

func runReconcile(baseDir string) (int, error) {
	return reconcileTickets(baseDir, false)
}

func reconcileTickets(baseDir string, dryRun bool) (int, error) {
	d, err := getDispatcher()
	if err != nil {
		return 0, err
	}
	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}
	maxStatusQueryFailures := cfg.MaxRetry
	if maxStatusQueryFailures <= 0 {
		maxStatusQueryFailures = 3
	}

	files, err := allTicketFiles(baseDir)
	if err != nil {
		return 0, err
	}

	actions := make([]string, 0)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}

		switch doc.Card.Status {
		case frontmatter.StatusDispatched:
			if doc.Card.DispatchID == nil {
				continue
			}

			statusResult, err := d.Status(*doc.Card.DispatchID)
			if err != nil {
				failureCount := consecutiveStatusQueryFailures(doc) + 1
				message := fmt.Sprintf("agent-mux status query failed (%d/%d): %v", failureCount, maxStatusQueryFailures, err)
				if failureCount < maxStatusQueryFailures {
					appendLog(doc, "warning -- "+message)
					if writeErr := doc.WriteFile(file); writeErr != nil {
						return 0, writeErr
					}
					actions = append(actions, fmt.Sprintf("%s: warning: %s", doc.Card.ID, message))
					continue
				}
				if dryRun {
					actions = append(actions, fmt.Sprintf("Would fail %s: %s", doc.Card.ID, message))
					continue
				}
				appendLog(doc, "warning -- "+message)
				action, failErr := failDuringReconcile(file, doc, cfg, fmt.Sprintf("agent-mux status query failed repeatedly: %v", err), "failed -- "+message)
				if failErr != nil {
					return 0, failErr
				}
				actions = append(actions, action)
				continue
			}

			action, err := reconcileDispatchedTicket(file, doc, statusResult, cfg, dryRun)
			if err != nil {
				return 0, err
			}
			if action != "" {
				actions = append(actions, action)
			}
		case frontmatter.StatusDone:
			if doc.Card.DispatchID == nil || doc.Card.Tokens != nil {
				continue
			}

			statusResult, err := d.Status(*doc.Card.DispatchID)
			if err != nil {
				continue
			}
			if statusResult.Tokens == nil {
				continue
			}

			if dryRun {
				actions = append(actions, fmt.Sprintf("Would backfill tokens for %s", doc.Card.ID))
				continue
			}

			doc.Card.Tokens = tokenUsage(statusResult.Tokens)
			if err := doc.WriteFile(file); err != nil {
				return 0, err
			}
			actions = append(actions, fmt.Sprintf("%s: backfilled tokens", doc.Card.ID))
		}
	}

	if len(actions) == 0 {
		fmt.Fprintln(stdout, "nothing to reconcile")
		return 0, nil
	}

	for _, action := range actions {
		fmt.Fprintln(stdout, action)
	}

	return len(actions), nil
}

func reconcileDispatchedTicket(file string, doc *frontmatter.Document, statusResult *dispatch.StatusResult, cfg config.Config, dryRun bool) (string, error) {
	switch statusResult.Status {
	case "running":
		return "", nil
	case "completed":
		resultText := strings.TrimSpace(doc.GetSection("Result"))
		if resultText == "" || isPlaceholder(resultText) {
			reason := "worker completed but no result found in ticket; check worker output paths"
			if resultText != "" {
				reason = "worker completed but result section still contains placeholder text; check worker output paths"
			}
			if dryRun {
				return fmt.Sprintf("Would fail %s: %s", doc.Card.ID, reason), nil
			}
			appendLog(doc, "warning -- "+reason)
			return failDuringReconcile(file, doc, cfg, reason, "failed -- "+reason)
		}

		if dryRun {
			return fmt.Sprintf("Would complete %s via reconcile", doc.Card.ID), nil
		}

		result, err := fsm.Apply(doc.Card.Status, fsm.TransitionComplete)
		if err != nil {
			return "", err
		}
		doc.Card.Status = result.To
		if statusResult.Tokens != nil {
			doc.Card.Tokens = tokenUsage(statusResult.Tokens)
		}
		appendLog(doc, "done -- completed via reconcile (agent didn't call tickets complete)")
		if doc.Card.Tokens != nil {
			appendLog(doc, fmt.Sprintf("tokens -- in=%d out=%d cache=%d peak_context=%d", doc.Card.Tokens.In, doc.Card.Tokens.Out, doc.Card.Tokens.Cache, doc.Card.Tokens.PeakContext))
		}
		if err := doc.WriteFile(file); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: done (via reconcile)", doc.Card.ID), nil
	case "failed":
		reason := "agent-mux reports failed"
		if strings.TrimSpace(statusResult.Error) != "" {
			reason = fmt.Sprintf("agent-mux reports failed: %s", strings.TrimSpace(statusResult.Error))
		}
		if dryRun {
			return fmt.Sprintf("Would fail %s: %s", doc.Card.ID, reason), nil
		}
		return failDuringReconcile(file, doc, cfg, reason, "failed -- "+reason)
	case "timeout":
		if dryRun {
			return fmt.Sprintf("Would fail %s: dispatch timed out", doc.Card.ID), nil
		}
		return failDuringReconcile(file, doc, cfg, "dispatch timed out", "failed -- dispatch timed out (reconcile)")
	default:
		return "", nil
	}
}

func failDuringReconcile(file string, doc *frontmatter.Document, cfg config.Config, message string, logLine string) (string, error) {
	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionFail)
	if err != nil {
		return "", err
	}
	doc.Card.Status = result.To
	if result.SetLastOutcome != "" {
		doc.Card.LastAttemptOutcome = stringPtr(result.SetLastOutcome)
	}
	appendLog(doc, logLine)

	maxRetry := cfg.MaxRetry
	if maxRetry == 0 {
		maxRetry = 3
	}
	if doc.Card.Attempts >= maxRetry {
		blockResult, err := fsm.Apply(doc.Card.Status, fsm.TransitionBlock)
		if err != nil {
			return "", err
		}
		doc.Card.Status = blockResult.To
		autoReason := fmt.Sprintf("Auto-blocked after %d failed attempts", maxRetry)
		doc.Card.BlockReason = &autoReason
		appendLog(doc, fmt.Sprintf("blocked -- auto-blocked after %d failed attempts", maxRetry))
	}

	if err := doc.WriteFile(file); err != nil {
		return "", err
	}
	actionSuffix := fmt.Sprintf("failed (%s)", message)
	if doc.Card.Status == frontmatter.StatusBlocked {
		actionSuffix = fmt.Sprintf("failed and auto-blocked (%s)", message)
	}
	return fmt.Sprintf("%s: %s", doc.Card.ID, actionSuffix), nil
}

func consecutiveStatusQueryFailures(doc *frontmatter.Document) int {
	lines := strings.Split(strings.TrimSpace(doc.GetSection("Log")), "\n")
	count := 0
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.Contains(line, "warning -- agent-mux status query failed") {
			count++
			continue
		}
		break
	}
	return count
}

func tokenUsage(tokens *dispatch.TokenData) *frontmatter.TokenUsage {
	return &frontmatter.TokenUsage{
		In:          tokens.In,
		Out:         tokens.Out,
		Cache:       tokens.Cache,
		PeakContext: tokens.PeakContext,
	}
}
