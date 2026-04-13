package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

func cmdComplete(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("complete")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets complete TICKET-ID")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets complete TICKET-ID")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status != frontmatter.StatusDispatched {
		return fmt.Errorf("ticket must be dispatched to complete: %s", doc.Card.Status)
	}
	resultText := strings.TrimSpace(doc.GetSection("Result"))
	if resultText == "" {
		return fmt.Errorf("Cannot complete %s — ## Result section is empty.\nWrite your result to the ticket card before completing.", id)
	}
	if isPlaceholder(resultText) {
		return fmt.Errorf("Cannot complete %s — ## Result section contains only placeholder text.\nWrite your actual results to the ticket card before completing.", id)
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionComplete)
	if err != nil {
		return err
	}
	doc.Card.Status = result.To

	if doc.Card.DispatchID != nil {
		d, err := getDispatcher()
		if err == nil {
			statusResult, err := d.Status(*doc.Card.DispatchID)
			if err == nil && statusResult.Tokens != nil {
				doc.Card.Tokens = &frontmatter.TokenUsage{
					In:          statusResult.Tokens.In,
					Out:         statusResult.Tokens.Out,
					Cache:       statusResult.Tokens.Cache,
					PeakContext: statusResult.Tokens.PeakContext,
				}
			}
		}
	} else {
		fmt.Fprintf(stderr, "warning: dispatch_id missing on card %s, token backfill skipped\n", id)
	}

	appendLog(doc, "done -- completed")
	if doc.Card.Tokens != nil {
		appendLog(doc, fmt.Sprintf("tokens -- in=%d out=%d cache=%d peak_context=%d", doc.Card.Tokens.In, doc.Card.Tokens.Out, doc.Card.Tokens.Cache, doc.Card.Tokens.PeakContext))
	}
	return doc.WriteFile(path)
}
