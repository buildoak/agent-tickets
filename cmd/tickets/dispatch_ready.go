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

	engineWeight := buildEngineWeightMap(files, cfg)

	eligible := make([]*frontmatter.Document, 0)
	for _, file := range files {
		doc, err := frontmatter.ParseFile(file)
		if err != nil {
			continue
		}
		if doc.Card.Status != frontmatter.StatusOpen || doc.Card.Manual {
			continue
		}
		if strings.TrimSpace(doc.GetSection("Scope")) == "" {
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
		if doc.Card.Status != frontmatter.StatusDispatched {
			continue
		}
		engine := valueOrBlank(doc.Card.Engine)
		model := valueOrBlank(doc.Card.Model)
		if engine == "" {
			continue
		}
		// When a ticket was dispatched via a profile that handles engine
		// selection, the card stores "profile-defined" instead of the real
		// engine/model. Resolve through the profile_engine/profile_model
		// config maps so the weight lands on the correct engine bucket.
		if engine == profileDefinedSentinel {
			profile := valueOrBlank(doc.Card.Profile)
			if profile != "" {
				engine = cfg.ResolveProfileEngine(profile)
			}
		}
		if model == profileDefinedSentinel {
			profile := valueOrBlank(doc.Card.Profile)
			if profile != "" {
				model = cfg.ResolveProfileModel(profile)
			}
		}
		if engine == "" {
			continue
		}
		weights[engine] += cfg.ModelWeightFor(model)
	}
	return weights
}
