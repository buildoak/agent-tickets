package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/fsm"
)

func cmdReopen(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("reopen")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets reopen TICKET-ID")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets reopen TICKET-ID")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionReopen)
	if err != nil {
		return err
	}

	doc.Card.Status = result.To
	if result.IncrementAttempts {
		doc.Card.Attempts++
	}
	if result.ClearDispatchFields {
		clearDispatchFields(&doc.Card)
		// On reopen, also clear card-spec dispatch fields so the ticket
		// can be re-dispatched with fresh parameters.
		doc.Card.Engine = nil
		doc.Card.Model = nil
		doc.Card.Effort = nil
		doc.Card.Tokens = nil
	}
	if result.ClearBlockReason {
		doc.Card.BlockReason = nil
	}
	if result.ArchiveResult {
		archived := strings.TrimSpace(doc.GetSection("Result"))
		if archived != "" {
			doc.AppendToSection("Log", fmt.Sprintf("- %s archived result:\n%s\n", timestamp(), archived))
		}
		doc.SetSection("Result", "\n")
	}

	appendLog(doc, fmt.Sprintf("open -- reopened, attempts=%d", doc.Card.Attempts))
	return doc.WriteFile(path)
}
