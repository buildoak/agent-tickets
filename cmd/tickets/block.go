package main

import (
	"fmt"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

func cmdBlock(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("block")
	reason := fs.String("reason", "", "block reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets block TICKET-ID --reason \"...\"")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets block TICKET-ID --reason \"...\"")
	}
	if *reason == "" {
		return fmt.Errorf("--reason is required")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status != frontmatter.StatusOpen && doc.Card.Status != frontmatter.StatusFailed {
		return fmt.Errorf("ticket must be open or failed to block: %s", doc.Card.Status)
	}

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionBlock)
	if err != nil {
		return err
	}
	doc.Card.Status = result.To
	if result.SetBlockReason {
		doc.Card.BlockReason = reason
	}
	appendLog(doc, fmt.Sprintf("blocked -- %s", *reason))
	return doc.WriteFile(path)
}
