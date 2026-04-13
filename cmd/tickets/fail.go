package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

func cmdFail(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("fail")
	reason := fs.String("reason", "", "failure reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets fail TICKET-ID --reason \"...\"")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets fail TICKET-ID --reason \"...\"")
	}
	if *reason == "" {
		return fmt.Errorf("--reason is required")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status != frontmatter.StatusDispatched {
		return fmt.Errorf("ticket must be dispatched to fail: %s", doc.Card.Status)
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionFail)
	if err != nil {
		return err
	}
	doc.Card.Status = result.To
	if result.SetLastOutcome != "" {
		doc.Card.LastAttemptOutcome = stringPtr(result.SetLastOutcome)
	}
	appendLog(doc, fmt.Sprintf("failed -- %s", *reason))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	maxRetry := cfg.MaxRetry
	if maxRetry == 0 {
		maxRetry = 3
	}
	if shouldAutoBlock(doc.Card.Attempts, maxRetry) {
		blockResult, err := fsm.Apply(doc.Card.Status, fsm.TransitionBlock)
		if err != nil {
			return err
		}
		doc.Card.Status = blockResult.To
		autoReason := fmt.Sprintf("Auto-blocked after %d failed attempts", maxRetry)
		doc.Card.BlockReason = &autoReason
		appendLog(doc, fmt.Sprintf("blocked -- auto-blocked after %d failed attempts", maxRetry))
	}

	return doc.WriteFile(path)
}
