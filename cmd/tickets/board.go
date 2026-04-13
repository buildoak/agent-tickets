package main

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/buildoak/agent-tickets/frontmatter"
)

type BoardEntry struct {
	frontmatter.Card
	Annotation string `json:"annotation"`
	Detail     string `json:"detail"`
}

func cmdBoard(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("board")
	initiative := fs.String("initiative", "", "initiative filter")
	asJSON := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets board [--initiative X] [--json]")
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

	statusByID := make(map[string]frontmatter.Status, len(files))
	failureByID := make(map[string]string, len(files))
	entries := make([]BoardEntry, 0, len(files))
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			return err
		}
		statusByID[doc.Card.ID] = doc.Card.Status
		failureByID[doc.Card.ID] = latestFailureReason(doc)
		entries = append(entries, BoardEntry{Card: doc.Card})
	}

	for i := range entries {
		entries[i].Annotation, entries[i].Detail = boardAnnotation(entries[i].Card, statusByID, failureByID)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	if *asJSON {
		return writeJSON(entries)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, entry := range entries {
		if entry.Detail != "" {
			if _, err := fmt.Fprintf(tw, "%s\t%s\t[%s]\t%s\t%s\n", entry.ID, entry.Title, entry.Tier, entry.Annotation, entry.Detail); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(tw, "%s\t%s\t[%s]\t%s\n", entry.ID, entry.Title, entry.Tier, entry.Annotation); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func boardAnnotation(card frontmatter.Card, statusByID map[string]frontmatter.Status, failureByID map[string]string) (string, string) {
	switch card.Status {
	case frontmatter.StatusOpen:
		if card.Manual {
			return "manual", ""
		}
		var unresolved []string
		for _, dep := range card.DependsOn {
			if statusByID[dep] != frontmatter.StatusDone {
				unresolved = append(unresolved, dep)
			}
		}
		for _, aw := range card.Awaits {
			if !statusByID[aw].IsTerminal() {
				unresolved = append(unresolved, aw+" (awaits)")
			}
		}
		if len(unresolved) > 0 {
			return "waiting", "-> " + strings.Join(unresolved, ", ")
		}
		return "queued", ""
	case frontmatter.StatusDispatched:
		return "running", strings.Trim(strings.Join([]string{valueOrBlank(card.Engine), valueOrBlank(card.Model)}, "/"), "/")
	case frontmatter.StatusDone:
		return "done", formatTokens(card.Tokens)
	case frontmatter.StatusFailed:
		detail := fmt.Sprintf("attempt %d", card.Attempts)
		if card.Attempts != 1 {
			detail = fmt.Sprintf("attempts %d", card.Attempts)
		}
		if reason := failureByID[card.ID]; reason != "" {
			detail += ": " + reason
		}
		return "failed", detail
	case frontmatter.StatusBlocked:
		return "blocked", valueOrBlank(card.BlockReason)
	default:
		return string(card.Status), ""
	}
}

func latestFailureReason(doc *frontmatter.Document) string {
	logSection := strings.TrimSpace(doc.GetSection("Log"))
	if logSection == "" {
		return ""
	}

	lines := strings.Split(logSection, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, "failed -- ") {
			continue
		}
		parts := strings.SplitN(line, "failed -- ", 2)
		if len(parts) != 2 {
			continue
		}
		return strings.TrimSpace(parts[1])
	}
	return ""
}

func formatTokens(tokens *frontmatter.TokenUsage) string {
	if tokens == nil {
		return ""
	}
	total := tokens.In + tokens.Out
	if total >= 1000 {
		return fmt.Sprintf("%.1fk tok", float64(total)/1000.0)
	}
	return fmt.Sprintf("%d tok", total)
}
