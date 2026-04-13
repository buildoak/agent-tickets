package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

func cmdCancel(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("cancel")
	reason := fs.String("reason", "", "cancel reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets cancel TICKET-ID --reason \"...\"")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets cancel TICKET-ID --reason \"...\"")
	}
	if *reason == "" {
		return fmt.Errorf("--reason is required")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status != frontmatter.StatusDispatched {
		return fmt.Errorf("ticket must be dispatched to cancel: %s", doc.Card.Status)
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionCancel)
	if err != nil {
		return err
	}
	doc.Card.Status = result.To
	if result.SetLastOutcome != "" {
		doc.Card.LastAttemptOutcome = stringPtr(result.SetLastOutcome)
	}
	if result.ClearDispatchFields {
		clearDispatchFields(&doc.Card)
	}
	appendLog(doc, fmt.Sprintf("open -- cancelled: %s", *reason))
	return doc.WriteFile(path)
}
