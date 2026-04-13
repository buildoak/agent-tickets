package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/buildoak/agent-tickets/frontmatter"
)

type InitiativeInfo struct {
	Initiative string         `json:"initiative" yaml:"initiative"`
	Title      string         `json:"title" yaml:"title"`
	Status     string         `json:"status" yaml:"status"`
	Created    string         `json:"created" yaml:"created"`
	Tickets    map[string]int `json:"tickets"`
}

func cmdInitiatives(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("initiatives")
	status := fs.String("status", "", "initiative status filter")
	asJSON := fs.Bool("json", false, "output json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets initiatives [--status active|paused|complete|archived] [--json]")
	}

	initiativesDir := filepath.Join(baseDir, "INITIATIVES")
	entries, err := os.ReadDir(initiativesDir)
	if err != nil {
		if os.IsNotExist(err) {
			entries = nil
		} else {
			return err
		}
	}

	infos := make([]InitiativeInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}

		initiative := strings.TrimSuffix(entry.Name(), ".md")
		cardPath := filepath.Join(initiativesDir, entry.Name())

		info, err := parseInitiativeCard(cardPath)
		if err != nil {
			return err
		}

		info.Tickets, err = countTicketsByStatus(baseDir, initiative)
		if err != nil {
			return err
		}
		if *status != "" && info.Status != *status {
			continue
		}

		infos = append(infos, *info)
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Initiative < infos[j].Initiative
	})

	if *asJSON {
		return writeJSON(infos)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "INITIATIVE\tTITLE\tSTATUS\tTICKETS"); err != nil {
		return err
	}
	for _, info := range infos {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", info.Initiative, info.Title, info.Status, formatTicketCounts(info.Tickets)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func parseInitiativeCard(path string) (*InitiativeInfo, error) {
	doc, err := frontmatter.ParseFile(path)
	if err != nil {
		return nil, err
	}
	return &InitiativeInfo{
		Initiative: doc.Card.Initiative,
		Title:      doc.Card.Title,
		Status:     string(doc.Card.Status),
		Created:    doc.Card.Created,
	}, nil
}

func countTicketsByStatus(baseDir, initiative string) (map[string]int, error) {
	dir := filepath.Join(baseDir, "cards", initiative)
	files, err := allTicketFiles(dir)
	if err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		counts[string(doc.Card.Status)]++
	}
	return counts, nil
}

func formatTicketCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "-"
	}

	order := []string{
		string(frontmatter.StatusOpen),
		string(frontmatter.StatusDispatched),
		string(frontmatter.StatusBlocked),
		string(frontmatter.StatusFailed),
		string(frontmatter.StatusDone),
	}

	parts := make([]string, 0, len(counts))
	for _, key := range order {
		if count := counts[key]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s:%d", key, count))
		}
	}

	var extras []string
	for key := range counts {
		if !slices.Contains(order, key) {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	for _, key := range extras {
		parts = append(parts, fmt.Sprintf("%s:%d", key, counts[key]))
	}

	return strings.Join(parts, " ")
}
