package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/buildoak/agent-tickets/frontmatter"
)

func cmdCreate(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("create")
	initiative := fs.String("initiative", "", "initiative")
	title := fs.String("title", "", "ticket title")
	tier := fs.String("tier", "", "ticket tier")
	manual := fs.Bool("manual", false, "manual ticket")
	dependsOn := fs.String("depends-on", "", "comma-separated dependencies")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets create --initiative X --title \"...\" --tier worker [--manual] [--depends-on A,B]")
	}
	if strings.TrimSpace(*initiative) == "" {
		return fmt.Errorf("--initiative is required")
	}
	if strings.TrimSpace(*title) == "" {
		return fmt.Errorf("--title is required")
	}
	if strings.TrimSpace(*tier) == "" {
		return fmt.Errorf("--tier is required")
	}

	initiativeDir, err := initiativeExists(baseDir, *initiative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(initiativeDir, 0o755); err != nil {
		return err
	}

	ticketTier := frontmatter.Tier(*tier)
	switch ticketTier {
	case frontmatter.TierWorker, frontmatter.TierDeep, frontmatter.TierHeavy:
	default:
		return fmt.Errorf("invalid tier: %s", *tier)
	}

	seq, err := nextSequence(initiativeDir, *initiative)
	if err != nil {
		return err
	}

	id := fmt.Sprintf("%s-%03d", *initiative, seq)
	deps := splitCSV(*dependsOn)
	if len(deps) > 0 {
		if err := validateDependencies(baseDir, id, deps); err != nil {
			return err
		}
	}

	doc := ticketTemplate(frontmatter.Card{
		ID:         id,
		Initiative: *initiative,
		Title:      *title,
		Status:     frontmatter.StatusOpen,
		Tier:       ticketTier,
		Tags:       []string{},
		Created:    dateOnly(),
		Manual:     *manual,
		DependsOn:  deps,
		Attempts:   0,
	})
	appendLog(doc, "open -- created")

	path := filepath.Join(initiativeDir, id+".md")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("error: %s already exists at %s — refusing to overwrite. Run 'tickets list --initiative %s' to see existing tickets", id, path, *initiative)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := doc.WriteFile(path); err != nil {
		return err
	}

	_, err = fmt.Fprintln(stdout, id)
	return err
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
