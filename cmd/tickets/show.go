package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdShow(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("show")
	asJSON := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets show TICKET-ID [--json]")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets show TICKET-ID [--json]")
	}

	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}

	if *asJSON {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		allFiles, err := allTicketFiles(baseDir)
		if err != nil {
			return err
		}
		statusByID := make(map[string]frontmatter.Status, len(allFiles))
		failureByID := make(map[string]string, len(allFiles))
		stallByID := make(map[string]string)
		now := time.Now()
		for _, file := range allFiles {
			allDoc, err := frontmatter.ParseFile(file)
			if err != nil {
				continue
			}
			statusByID[allDoc.Card.ID] = allDoc.Card.Status
			failureByID[allDoc.Card.ID] = latestFailureReason(allDoc)
			if allDoc.Card.Status == frontmatter.StatusDispatched {
				if ann := stallAnnotation(allDoc, cfg, now); ann != "" {
					stallByID[allDoc.Card.ID] = ann
				}
			}
		}
		annotation, detail := boardAnnotation(doc.Card, statusByID, failureByID, stallByID, cfg)
		return writeJSON(BoardEntry{Card: doc.Card, Annotation: annotation, Detail: detail})
	}

	data, err := doc.Serialize()
	if err != nil {
		return err
	}

	_ = path
	_, err = fmt.Fprint(stdout, string(data))
	return err
}
