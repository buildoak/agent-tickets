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
				fmt.Fprintf(stderr, "warning: dispatch_id missing on dispatched card %s, skipping reconcile\n", doc.Card.ID)
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
				appendLog(doc, "note -- auto-failed due to status-check infrastructure failure, not confirmed worker failure")
				action, failErr := failDuringReconcile(file, doc, nil, fmt.Sprintf("agent-mux status query failed repeatedly: %v", err), "failed -- "+message)
				if failErr != nil {
					return 0, failErr
				}
				actions = append(actions, action)
				fmt.Fprintf(stdout, "[STALL_WARNING] %s auto-failed due to status-check failure (infrastructure may be degraded)\n", doc.Card.ID)
				continue
			}

			// Backfill session_id when the status response provides one
			// but the card doesn't have it yet (agent-mux --async only
			// returns dispatch_id; session_id becomes available later).
			sessionBackfilled := false
			if statusResult.SessionID != "" && (doc.Card.SessionID == nil || *doc.Card.SessionID == "") {
				if dryRun {
					actions = append(actions, fmt.Sprintf("Would backfill session_id for %s", doc.Card.ID))
				} else {
					doc.Card.SessionID = stringPtr(statusResult.SessionID)
					sessionBackfilled = true
				}
			}

			action, err := reconcileDispatchedTicket(file, doc, statusResult, dryRun)
			if err != nil {
				return 0, err
			}
			if action != "" {
				actions = append(actions, action)
			}

			// If we backfilled session_id but the ticket is still running
			// (no state transition wrote the file), persist the update now.
			if sessionBackfilled && action == "" {
				if err := doc.WriteFile(file); err != nil {
					return 0, err
				}
				actions = append(actions, fmt.Sprintf("%s: backfilled session_id", doc.Card.ID))
			}
		case frontmatter.StatusDone, frontmatter.StatusFailed:
			if doc.Card.DispatchID == nil {
				// Legacy card completed before dispatch tracking existed; skip silently.
				continue
			}

			needsTokens := doc.Card.Tokens == nil
			needsSession := doc.Card.SessionID == nil || *doc.Card.SessionID == ""
			if !needsTokens && !needsSession {
				continue
			}

			statusResult, err := d.Status(*doc.Card.DispatchID)
			if err != nil {
				continue
			}

			var filled []string
			if needsTokens && statusResult.Tokens != nil {
				doc.Card.Tokens = tokenUsage(statusResult.Tokens)
				filled = append(filled, "tokens")
			}
			if needsSession && statusResult.SessionID != "" {
				doc.Card.SessionID = stringPtr(statusResult.SessionID)
				filled = append(filled, "session_id")
			}
			if len(filled) == 0 {
				continue
			}

			if dryRun {
				actions = append(actions, fmt.Sprintf("Would backfill %s for %s", strings.Join(filled, ", "), doc.Card.ID))
				continue
			}

			if err := doc.WriteFile(file); err != nil {
				return 0, err
			}
			actions = append(actions, fmt.Sprintf("%s: backfilled %s", doc.Card.ID, strings.Join(filled, ", ")))
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

// hasSubstantialResult checks whether the card's ## Result section contains
// meaningful content (>50 non-whitespace characters, not a placeholder).
func hasSubstantialResult(doc *frontmatter.Document) bool {
	raw := doc.GetSection("Result")
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || isPlaceholder(trimmed) {
		return false
	}
	// Count non-whitespace characters.
	count := 0
	for _, r := range trimmed {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			count++
		}
	}
	return count > 50
}

func reconcileDispatchedTicket(file string, doc *frontmatter.Document, statusResult *dispatch.StatusResult, dryRun bool) (string, error) {
	hasResult := hasSubstantialResult(doc)

	switch statusResult.EffectiveStatus() {
	case "running":
		return "", nil
	case "completed":
		if !hasResult {
			// Agent-mux says completed but the worker never wrote a Result.
			reason := "worker completed without writing Result"
			if dryRun {
				return fmt.Sprintf("Would fail %s: %s", doc.Card.ID, reason), nil
			}
			return failDuringReconcile(file, doc, statusResult, reason, "failed -- "+reason)
		}
		if dryRun {
			return fmt.Sprintf("Would complete %s via reconcile", doc.Card.ID), nil
		}

		result, err := fsm.Apply(doc.Card.Status, fsm.TransitionComplete)
		if err != nil {
			return "", err
		}
		doc.Card.Status = result.To
		backfillStatusFields(doc, statusResult)
		if statusResult.Tokens != nil {
			doc.Card.Tokens = tokenUsage(statusResult.Tokens)
		}
		appendLog(doc, "done -- completed via reconcile (agent didn't call tickets complete)")
		if err := doc.WriteFile(file); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: done (via reconcile)", doc.Card.ID), nil
	case "failed":
		if hasResult {
			// Worker crashed but did write a Result — count it as done.
			if dryRun {
				return fmt.Sprintf("Would complete %s via reconcile (result present despite failure)", doc.Card.ID), nil
			}
			result, err := fsm.Apply(doc.Card.Status, fsm.TransitionComplete)
			if err != nil {
				return "", err
			}
			doc.Card.Status = result.To
			backfillStatusFields(doc, statusResult)
			if statusResult.Tokens != nil {
				doc.Card.Tokens = tokenUsage(statusResult.Tokens)
			}
			appendLog(doc, "done -- agent-mux reports failed but Result section is populated; marking done")
			if err := doc.WriteFile(file); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s: done (via reconcile, result present despite failure)", doc.Card.ID), nil
		}
		reason := "agent-mux reports failed"
		if strings.TrimSpace(statusResult.Error) != "" {
			reason = fmt.Sprintf("agent-mux reports failed: %s", strings.TrimSpace(statusResult.Error))
		}
		if dryRun {
			return fmt.Sprintf("Would fail %s: %s", doc.Card.ID, reason), nil
		}
		return failDuringReconcile(file, doc, statusResult, reason, "failed -- "+reason)
	case "timeout":
		if hasResult {
			// Timed out but worker wrote a Result — count it as done.
			if dryRun {
				return fmt.Sprintf("Would complete %s via reconcile (result present despite timeout)", doc.Card.ID), nil
			}
			result, err := fsm.Apply(doc.Card.Status, fsm.TransitionComplete)
			if err != nil {
				return "", err
			}
			doc.Card.Status = result.To
			backfillStatusFields(doc, statusResult)
			if statusResult.Tokens != nil {
				doc.Card.Tokens = tokenUsage(statusResult.Tokens)
			}
			appendLog(doc, "done -- dispatch timed out but Result section is populated; marking done")
			if err := doc.WriteFile(file); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s: done (via reconcile, result present despite timeout)", doc.Card.ID), nil
		}
		if dryRun {
			return fmt.Sprintf("Would fail %s: dispatch timed out", doc.Card.ID), nil
		}
		return failDuringReconcile(file, doc, statusResult, "dispatch timed out", "failed -- dispatch timed out (reconcile)")
	default:
		return "", nil
	}
}

func failDuringReconcile(file string, doc *frontmatter.Document, statusResult *dispatch.StatusResult, message string, logLine string) (string, error) {
	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionFail)
	if err != nil {
		return "", err
	}
	doc.Card.Status = result.To
	backfillStatusFields(doc, statusResult)
	if result.SetLastOutcome != "" {
		doc.Card.LastAttemptOutcome = stringPtr(result.SetLastOutcome)
	}
	appendLog(doc, logLine)
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	maxRetry := cfg.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 3
	}
	if shouldAutoBlock(doc.Card.Attempts, maxRetry) {
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
	return fmt.Sprintf("%s: failed (%s)", doc.Card.ID, message), nil
}

func backfillStatusFields(doc *frontmatter.Document, statusResult *dispatch.StatusResult) {
	if statusResult == nil {
		return
	}
	if statusResult.SessionID != "" {
		doc.Card.SessionID = stringPtr(statusResult.SessionID)
	}
	if statusResult.Tokens != nil {
		doc.Card.Tokens = tokenUsage(statusResult.Tokens)
	}
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

