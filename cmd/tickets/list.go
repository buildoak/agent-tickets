package main

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdList(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("list")
	initiative := fs.String("initiative", "", "initiative filter")
	status := fs.String("status", "", "status filter")
	tag := fs.String("tag", "", "tag filter")
	asJSON := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets list [--initiative X] [--status Y] [--tag Z] [--json]")
	}

	var files []string
	if *initiative != "" {
		dir, err := initiativeExists(baseDir, *initiative)
		if err != nil {
			return err
		}
		files, err = allTicketFiles(dir)
		if err != nil {
			return err
		}
	} else {
		files, err = allTicketFiles(baseDir)
		if err != nil {
			return err
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	now := time.Now()
	cards := make([]frontmatter.Card, 0, len(files))
	stallByID := make(map[string]string)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			return err
		}
		if *status != "" && string(doc.Card.Status) != *status {
			continue
		}
		if *tag != "" && !hasTag(doc.Card.Tags, *tag) {
			continue
		}
		if doc.Card.Status == frontmatter.StatusDispatched {
			if ann := stallAnnotation(doc, cfg, now); ann != "" {
				stallByID[doc.Card.ID] = ann
			}
		}
		cards = append(cards, doc.Card)
	}

	if *asJSON {
		allFiles, err := allTicketFiles(baseDir)
		if err != nil {
			return err
		}
		statusByID := make(map[string]frontmatter.Status, len(allFiles))
		failureByID := make(map[string]string, len(allFiles))
		for _, file := range allFiles {
			doc, err := frontmatter.ParseFile(file)
			if err != nil {
				continue
			}
			statusByID[doc.Card.ID] = doc.Card.Status
			failureByID[doc.Card.ID] = latestFailureReason(doc)
			if doc.Card.Status == frontmatter.StatusDispatched {
				if ann := stallAnnotation(doc, cfg, now); ann != "" {
					stallByID[doc.Card.ID] = ann
				}
			}
		}

		entries := make([]BoardEntry, 0, len(cards))
		for _, card := range cards {
			annotation, detail := boardAnnotation(card, statusByID, failureByID, stallByID, cfg)
			entries = append(entries, BoardEntry{Card: card, Annotation: annotation, Detail: detail})
		}
		return writeJSON(entries)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tTITLE\tTIER\tSTATUS"); err != nil {
		return err
	}
	for _, card := range cards {
		statusStr := string(card.Status)
		if stall, ok := stallByID[card.ID]; ok {
			statusStr += " " + stall
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", card.ID, card.Title, card.Tier, statusStr); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// hasTag is defined in helpers.go (shared utility used by list.go and stall.go).
