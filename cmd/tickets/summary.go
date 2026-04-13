package main

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/buildoak/agent-tickets/frontmatter"
)

type SummaryRow struct {
	Initiative string `json:"initiative"`
	Open       int    `json:"open"`
	Dispatched int    `json:"dispatched"`
	Done       int    `json:"done"`
	Failed     int    `json:"failed"`
	Blocked    int    `json:"blocked"`
	Closed     int    `json:"closed"`
	Total      int    `json:"total"`
}

func cmdSummary(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	fs := newFlagSet("summary")
	asJSON := fs.Bool("json", false, "output json array")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets summary [--json]")
	}

	files, err := allTicketFiles(baseDir)
	if err != nil {
		return err
	}

	counts := make(map[string]*SummaryRow)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			return err
		}
		initiative := doc.Card.Initiative
		if initiative == "" {
			// fall back to parsing from ID
			initiative, _, _ = parseTicketID(doc.Card.ID)
		}
		if initiative == "" {
			continue
		}
		row, ok := counts[initiative]
		if !ok {
			row = &SummaryRow{Initiative: initiative}
			counts[initiative] = row
		}
		switch doc.Card.Status {
		case frontmatter.StatusOpen:
			row.Open++
		case frontmatter.StatusDispatched:
			row.Dispatched++
		case frontmatter.StatusDone:
			row.Done++
		case frontmatter.StatusFailed:
			row.Failed++
		case frontmatter.StatusBlocked:
			row.Blocked++
		case frontmatter.StatusClosed:
			row.Closed++
		}
		row.Total++
	}

	// sort by initiative name
	initiatives := make([]string, 0, len(counts))
	for k := range counts {
		initiatives = append(initiatives, k)
	}
	sort.Strings(initiatives)

	rows := make([]SummaryRow, 0, len(initiatives))
	totals := SummaryRow{Initiative: "TOTAL"}
	for _, init := range initiatives {
		r := counts[init]
		rows = append(rows, *r)
		totals.Open += r.Open
		totals.Dispatched += r.Dispatched
		totals.Done += r.Done
		totals.Failed += r.Failed
		totals.Blocked += r.Blocked
		totals.Closed += r.Closed
		totals.Total += r.Total
	}

	if *asJSON {
		return writeJSON(rows)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "Initiative\topen\tdispatched\tdone\tfailed\tblocked\tclosed\ttotal"); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
			r.Initiative, r.Open, r.Dispatched, r.Done, r.Failed, r.Blocked, r.Closed, r.Total); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
		totals.Initiative, totals.Open, totals.Dispatched, totals.Done, totals.Failed, totals.Blocked, totals.Closed, totals.Total); err != nil {
		return err
	}
	return tw.Flush()
}
