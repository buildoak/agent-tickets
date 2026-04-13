package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/fsm"
)

func cmdClose(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("close")
	reason := fs.String("reason", "", "closure reason (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets close TICKET-ID --reason \"...\"")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets close TICKET-ID --reason \"...\"")
	}
	if *reason == "" {
		return fmt.Errorf("--reason is required")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionClose)
	if err != nil {
		return err
	}

	doc.Card.Status = result.To
	if result.ClearDispatchFields {
		clearDispatchFields(&doc.Card)
	}
	if result.ArchiveResult {
		archived := strings.TrimSpace(doc.GetSection("Result"))
		if archived != "" {
			doc.AppendToSection("Log", fmt.Sprintf("- %s archived result:\n%s\n", timestamp(), archived))
		}
		doc.SetSection("Result", "\n")
	}
	doc.Card.LastAttemptOutcome = reason

	appendLog(doc, fmt.Sprintf("closed -- %s", *reason))
	return doc.WriteFile(path)
}
