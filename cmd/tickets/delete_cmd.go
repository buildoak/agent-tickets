package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdDelete(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("delete")
	cascade := fs.Bool("cascade", false, "cascade delete")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets delete TICKET-ID [--cascade]")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets delete TICKET-ID [--cascade]")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status == frontmatter.StatusDispatched {
		return fmt.Errorf("cannot delete dispatched ticket: %s", id)
	}

	dependents, err := dependentMap(baseDir)
	if err != nil {
		return err
	}
	if len(dependents[id]) > 0 && !*cascade {
		deps := dependents[id]
		msg := fmt.Sprintf("cannot delete %s - %s depends on it.\n", id, deps[0])
		if len(deps) > 1 {
			msg = fmt.Sprintf("cannot delete %s - %d tickets depend on it: %s\n", id, len(deps), stringsJoin(deps))
		}
		msg += "Delete the branch? This will also delete:\n"
		all := collectCascadeIDs(id, dependents, map[string]struct{}{})
		for _, cascadeID := range all {
			if cascadeID != id {
				msg += fmt.Sprintf("  - %s\n", cascadeID)
			}
		}
		msg += fmt.Sprintf("Confirm with: tickets delete %s --cascade", id)
		return fmt.Errorf("%s", msg)
	}

	targets := []string{id}
	if *cascade {
		targets = collectCascadeIDs(id, dependents, map[string]struct{}{})
		sort.Strings(targets)
		var dispatched []string
		for _, target := range targets {
			_, targetDoc, err := loadTicket(baseDir, target)
			if err != nil {
				return err
			}
			if targetDoc.Card.Status == frontmatter.StatusDispatched {
				dispatched = append(dispatched, target)
			}
		}
		if len(dispatched) > 0 {
			return fmt.Errorf("cannot cascade delete %s: dispatched descendants block deletion: %s", id, stringsJoin(dispatched))
		}
	}

	for _, target := range targets {
		targetPath := path
		if target != id {
			targetPath, err = findTicketFile(baseDir, target)
			if err != nil {
				return err
			}
		}
		if err := os.Remove(targetPath); err != nil {
			return err
		}
	}

	return nil
}

func stringsJoin(items []string) string {
	return strings.Join(items, ", ")
}
