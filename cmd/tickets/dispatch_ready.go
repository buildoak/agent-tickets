package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

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
	docs, err := loadAllTicketDocs(baseDir)
	if err != nil {
		return 0, err
	}
	return dispatchReadyTicketsFromDocs(baseDir, docs, maxDispatch, dryRun)
}

// dispatchReadyTicketsFromDocs dispatches eligible open tickets using a
// pre-parsed slice of ticket docs. Dependency and awaits lookups use the
// slice as an in-memory index instead of re-reading each card's file.
func dispatchReadyTicketsFromDocs(baseDir string, docs []TicketDoc, maxDispatch int, dryRun bool) (int, error) {
	if maxDispatch <= 0 {
		fmt.Fprintln(stdout, "nothing to dispatch")
		return 0, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}

	engineWeight := buildEngineWeightMapFromDocs(docs, cfg)

	byID := make(map[string]*frontmatter.Document, len(docs))
	for _, td := range docs {
		byID[td.Doc.Card.ID] = td.Doc
	}

	eligible := make([]*frontmatter.Document, 0)
	for _, td := range docs {
		doc := td.Doc
		if doc.Card.Status != frontmatter.StatusOpen || doc.Card.Manual {
			continue
		}
		if strings.TrimSpace(doc.GetSection("Scope")) == "" {
			continue
		}

		ready := true
		for _, dep := range doc.Card.DependsOn {
			depDoc, ok := byID[dep]
			if !ok || depDoc.Card.Status != frontmatter.StatusDone {
				ready = false
				break
			}
		}
		if ready {
			for _, aw := range doc.Card.Awaits {
				awDoc, ok := byID[aw]
				if !ok || !awDoc.Card.Status.IsTerminal() {
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

	var d dispatch.Dispatcher
	if !dryRun {
		d, err = getDispatcher()
		if err != nil {
			return 0, err
		}
	}
	dispatched := 0

	for i, doc := range eligible {
		if dispatched >= maxDispatch {
			break
		}

		resolvedOpts, err := resolveDispatchOptions(baseDir, doc.Card, dispatch.DispatchOptions{}, cfg)
		if err != nil {
			fmt.Fprintf(stdout, "%s: error: %v\n", doc.Card.ID, err)
			continue
		}

		// Resolve the actual engine for cap checking. When engine falls
		// through to config defaults but a profile defines a specific engine
		// (e.g. paper-ops-worker → gemini), use the profile's engine.
		capEngine := resolvedOpts.Engine
		capModel := resolvedOpts.Model
		if resolvedOpts.EngineSource == dispatch.SourceConfig && resolvedOpts.Profile != "" {
			if pe := cfg.ResolveProfileEngine(resolvedOpts.Profile); pe != "" {
				capEngine = pe
			}
		}
		if resolvedOpts.ModelSource == dispatch.SourceConfig && resolvedOpts.Profile != "" {
			if pm := cfg.ResolveProfileModel(resolvedOpts.Profile); pm != "" {
				capModel = pm
			}
		}

		candidateWeight := cfg.ModelWeightFor(capModel)
		engineCap := cfg.EngineCap(capEngine)
		if engineCap >= 0 {
			currentWeight := engineWeight[capEngine]
			if currentWeight+candidateWeight > engineCap {
				fmt.Fprintf(stdout, "%s: skipped (engine %s weight %d+%d > cap %d)\n",
					doc.Card.ID, capEngine, currentWeight, candidateWeight, engineCap)
				continue
			}
		}

		if dryRun {
			fmt.Fprintf(stdout, "Would dispatch %s\n", doc.Card.ID)
			dispatched++
			engineWeight[capEngine] += candidateWeight
			continue
		}

		result, err := dispatchTicket(baseDir, doc.Card.ID, d, cfg, dispatch.DispatchOptions{})
		if err != nil {
			fmt.Fprintf(stdout, "%s: error: %v\n", doc.Card.ID, err)
			continue
		}
		fmt.Fprintf(stdout, "%s: dispatched dispatch_id=%s session_id=%s\n", doc.Card.ID, result.DispatchID, result.SessionID)
		dispatched++
		engineWeight[capEngine] += candidateWeight

		if cfg.StaggerSeconds > 0 && dispatched < maxDispatch && i < len(eligible)-1 {
			fmt.Fprintf(stdout, "staggering %ds before next dispatch\n", cfg.StaggerSeconds)
			time.Sleep(time.Duration(cfg.StaggerSeconds) * time.Second)
		}
	}

	if dryRun {
		return dispatched, nil
	}
	if dispatched == 0 {
		fmt.Fprintln(stdout, "nothing to dispatch")
	}
	return dispatched, nil
}

// buildEngineWeightMap sums the model weight of all in-flight dispatched tickets by engine.
func buildEngineWeightMap(files []string, cfg config.Config) map[string]int {
	weights := make(map[string]int)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		weights = addCardToEngineWeight(weights, &doc.Card, cfg)
	}
	return weights
}

// buildEngineWeightMapFromDocs is the single-pass variant used by tick
// phases that already have parsed docs in memory.
func buildEngineWeightMapFromDocs(docs []TicketDoc, cfg config.Config) map[string]int {
	weights := make(map[string]int)
	for _, td := range docs {
		weights = addCardToEngineWeight(weights, &td.Doc.Card, cfg)
	}
	return weights
}

func addCardToEngineWeight(weights map[string]int, card *frontmatter.Card, cfg config.Config) map[string]int {
	if card.Status != frontmatter.StatusDispatched {
		return weights
	}
	engine := valueOrBlank(card.Engine)
	model := valueOrBlank(card.Model)
	if engine == "" {
		return weights
	}
	// When a ticket was dispatched via a profile that handles engine
	// selection, the card stores "profile-defined" instead of the real
	// engine/model. Resolve through the profile_engine/profile_model
	// config maps so the weight lands on the correct engine bucket.
	if engine == profileDefinedSentinel {
		profile := valueOrBlank(card.Profile)
		if profile != "" {
			engine = cfg.ResolveProfileEngine(profile)
		}
	}
	if model == profileDefinedSentinel {
		profile := valueOrBlank(card.Profile)
		if profile != "" {
			model = cfg.ResolveProfileModel(profile)
		}
	}
	if engine == "" {
		return weights
	}
	weights[engine] += cfg.ModelWeightFor(model)
	return weights
}
