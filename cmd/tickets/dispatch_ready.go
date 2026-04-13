package main

import (
	"fmt"
	"slices"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdDispatchReady(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("dispatch-ready")
	maxDispatch := fs.Int("max", 5, "max tickets to dispatch")
	dryRun := fs.Bool("dry-run", false, "preview without dispatching")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets dispatch-ready [--max N] [--dry-run]")
	}

	_, err = dispatchReadyTickets(baseDir, *maxDispatch, *dryRun)
	return err
}

func runDispatchReady(baseDir string, maxDispatch int) (int, error) {
	return dispatchReadyTickets(baseDir, maxDispatch, false)
}

func dispatchReadyTickets(baseDir string, maxDispatch int, dryRun bool) (int, error) {
	if maxDispatch <= 0 {
		fmt.Fprintln(stdout, "nothing to dispatch")
		return 0, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}

	files, err := allTicketFiles(baseDir)
	if err != nil {
		return 0, err
	}

	eligible := make([]*frontmatter.Document, 0)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		if doc.Card.Status != frontmatter.StatusOpen || doc.Card.Manual {
			continue
		}

		ready := true
		for _, dep := range doc.Card.DependsOn {
			_, depDoc, err := loadTicket(baseDir, dep)
			if err != nil || depDoc.Card.Status != frontmatter.StatusDone {
				ready = false
				break
			}
		}
		if ready {
			for _, aw := range doc.Card.Awaits {
				_, awDoc, err := loadTicket(baseDir, aw)
				if err != nil || !awDoc.Card.Status.IsTerminal() {
					ready = false
					break
				}
			}
		}
		if ready {
			eligible = append(eligible, doc)
		}
	}

	slices.SortFunc(eligible, func(a, b *frontmatter.Document) int {
		if a.Card.Created != b.Card.Created {
			if a.Card.Created < b.Card.Created {
				return -1
			}
			return 1
		}
		if a.Card.ID < b.Card.ID {
			return -1
		}
		if a.Card.ID > b.Card.ID {
			return 1
		}
		return 0
	})

	if len(eligible) == 0 {
		fmt.Fprintln(stdout, "nothing to dispatch")
		return 0, nil
	}

	limit := min(maxDispatch, len(eligible))
	var d dispatch.Dispatcher
	if !dryRun {
		d, err = getDispatcher()
		if err != nil {
			return 0, err
		}
	}
	dispatched := 0

	for _, doc := range eligible[:limit] {
		if _, err := resolveDispatchOptions(doc.Card, dispatch.DispatchOptions{}, cfg); err != nil {
			fmt.Fprintf(stdout, "%s: error: %v\n", doc.Card.ID, err)
			continue
		}
		if dryRun {
			fmt.Fprintf(stdout, "Would dispatch %s\n", doc.Card.ID)
			continue
		}

		result, err := dispatchTicket(baseDir, doc.Card.ID, d, cfg, dispatch.DispatchOptions{})
		if err != nil {
			fmt.Fprintf(stdout, "%s: error: %v\n", doc.Card.ID, err)
			continue
		}
		fmt.Fprintf(stdout, "%s: dispatched dispatch_id=%s session_id=%s\n", doc.Card.ID, result.DispatchID, result.SessionID)
		dispatched++
	}

	if dryRun {
		return limit, nil
	}
	return dispatched, nil
}
