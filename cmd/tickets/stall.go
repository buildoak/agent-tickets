package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
)

type StalledTicket struct {
	ID           string
	Initiative   string
	DispatchID   string
	Engine       string
	Tier         string
	FilePath     string
	DispatchedAt time.Time
	StalledFor   time.Duration
	TimeoutMin   int
}

func runStallDetection(baseDir string, cfg config.Config) (int, error) {
	files, err := allTicketFiles(baseDir)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	var stalled []StalledTicket

	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		if doc.Card.Status != frontmatter.StatusDispatched {
			continue
		}

		dispatchedAt := resolveDispatchTimestamp(doc)
		if dispatchedAt.IsZero() {
			continue
		}

		timeoutMin := cfg.StallTimeoutForTicket(doc.Card.Initiative, string(doc.Card.Tier))
		timeout := time.Duration(timeoutMin) * time.Minute
		elapsed := now.Sub(dispatchedAt)
		if elapsed <= timeout {
			continue
		}

		stalled = append(stalled, StalledTicket{
			ID:           doc.Card.ID,
			Initiative:   doc.Card.Initiative,
			DispatchID:   valueOrBlank(doc.Card.DispatchID),
			Engine:       valueOrBlank(doc.Card.Engine),
			Tier:         string(doc.Card.Tier),
			FilePath:     file,
			DispatchedAt: dispatchedAt,
			StalledFor:   elapsed,
			TimeoutMin:   timeoutMin,
		})
	}

	if len(stalled) == 0 {
		return 0, nil
	}

	fmt.Fprintf(stdout, "[STALL_WARNING] %d stalled ticket(s):\n", len(stalled))
	for _, s := range stalled {
		fmt.Fprintf(stdout, "  %s: stalled %.0fm (timeout %dm, engine=%s, dispatch_id=%s)\n",
			s.ID, s.StalledFor.Minutes(), s.TimeoutMin, s.Engine, s.DispatchID)
	}

	// Auto-fail stalled tickets — the tier/initiative timeout IS the auto-fail threshold
	for _, s := range stalled {
		doc, err := frontmatter.ParseFile(s.FilePath)
		if err != nil {
			continue
		}
		reason := fmt.Sprintf("auto-failed: stalled %.0fm (threshold %dm)", s.StalledFor.Minutes(), s.TimeoutMin)
		msg, err := failDuringReconcile(s.FilePath, doc, nil, reason, "failed -- "+reason)
		if err != nil {
			continue
		}
		fmt.Fprintf(stdout, "%s\n", msg)
	}

	return len(stalled), nil
}

func resolveDispatchTimestamp(doc *frontmatter.Document) time.Time {
	if doc.Card.DispatchedAt != nil && *doc.Card.DispatchedAt != "" {
		if t, err := time.Parse(time.RFC3339, *doc.Card.DispatchedAt); err == nil {
			return t
		}
	}

	lines := strings.Split(doc.GetSection("Log"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, "dispatched --") {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		if t, err := time.Parse(time.RFC3339, parts[0]); err == nil {
			return t
		}
	}

	return time.Time{}
}

