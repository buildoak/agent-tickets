package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
)

const maxMigrateRewriteScope = 100

func cmdMigrate(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	targetInitiative := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		targetInitiative = args[0]
		args = args[1:]
	}

	fs := newFlagSet("migrate")
	dryRun := fs.Bool("dry-run", false, "preview migrate changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" || targetInitiative == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets migrate TICKET-ID TARGET-INITIATIVE [--dry-run]")
	}

	if _, _, err := parseTicketID(id); err != nil {
		return err
	}

	targetDir, err := initiativeExists(baseDir, targetInitiative)
	if err != nil {
		return err
	}

	oldPath, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return err
	}
	if doc.Card.Status == frontmatter.StatusDispatched {
		return fmt.Errorf("cannot migrate dispatched ticket: %s", id)
	}

	nextSeq, err := nextSequence(targetDir, targetInitiative)
	if err != nil {
		return err
	}

	newID := fmt.Sprintf("%s-%03d", targetInitiative, nextSeq)
	newPath := filepath.Join(targetDir, newID+".md")

	doc.Card.ID = newID
	doc.Card.Initiative = targetInitiative

	type dependsUpdate struct {
		path string
		id   string
		doc  *frontmatter.Document
	}

	var updates []dependsUpdate
	files, err := allTicketFiles(baseDir)
	if err != nil {
		return err
	}
	for _, path := range files {
		if path == oldPath {
			continue
		}

		depDoc, err := frontmatter.ParseFile(path)
		if err != nil {
			return err
		}

		changed := false
		for i, dep := range depDoc.Card.DependsOn {
			if dep != id {
				continue
			}
			depDoc.Card.DependsOn[i] = newID
			changed = true
		}
		for i, aw := range depDoc.Card.Awaits {
			if aw != id {
				continue
			}
			depDoc.Card.Awaits[i] = newID
			changed = true
		}
		if !changed {
			continue
		}
		if depDoc.Card.Status == frontmatter.StatusDispatched {
			return fmt.Errorf("cannot migrate %s: dependent %s is dispatched", id, depDoc.Card.ID)
		}

		updates = append(updates, dependsUpdate{
			path: path,
			id:   depDoc.Card.ID,
			doc:  depDoc,
		})
	}
	if len(updates) > maxMigrateRewriteScope {
		var affected []string
		for _, update := range updates {
			affected = append(affected, update.id)
		}
		return fmt.Errorf("refusing migrate: depends_on rewrite scope too large (%d tickets): %s", len(updates), strings.Join(affected, ", "))
	}

	graph, err := buildDepGraph(baseDir)
	if err != nil {
		return err
	}
	delete(graph, id)
	edges := append([]string(nil), doc.Card.DependsOn...)
	edges = append(edges, doc.Card.Awaits...)
	graph[newID] = edges
	for _, update := range updates {
		uEdges := append([]string(nil), update.doc.Card.DependsOn...)
		uEdges = append(uEdges, update.doc.Card.Awaits...)
		graph[update.id] = uEdges
	}

	if err := validateDependencyGraph(graph, newID, doc.Card.DependsOn); err != nil {
		return err
	}
	for _, update := range updates {
		if err := validateDependencyGraph(graph, update.id, update.doc.Card.DependsOn); err != nil {
			return err
		}
	}

	if *dryRun {
		if _, err := fmt.Fprintf(stdout, "Would migrate %s -> %s\n", id, newID); err != nil {
			return err
		}
		for _, update := range updates {
			if _, err := fmt.Fprintf(stdout, "Would update depends_on in %s: %s -> %s\n", update.id, id, newID); err != nil {
				return err
			}
		}
		return nil
	}

	if err := doc.WriteFile(newPath); err != nil {
		return err
	}
	for _, update := range updates {
		if err := update.doc.WriteFile(update.path); err != nil {
			return err
		}
	}
	if err := os.Remove(oldPath); err != nil {
		return err
	}

	_, err = fmt.Fprintf(stdout, "migrated %s -> %s\n", id, newID)
	return err
}
