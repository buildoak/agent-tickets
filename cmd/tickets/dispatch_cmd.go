package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

// multiIDStaggerFloor is the minimum inter-dispatch delay (seconds) applied
// when >1 ticket is dispatched in a single invocation and the user did not
// explicitly override --stagger-seconds. Exists because agent-mux --async
// does not daemonize: the child stays attached to the tickets process and
// is SIGKILL'd on parent exit via kqueue EVFILT_PROC|NOTE_EXIT. Without a
// delay, firing N dispatches back-to-back then exiting tickets kills N-1
// codex workers mid-startup.
const multiIDStaggerFloor = 15

var dispatcher dispatch.Dispatcher

func getDispatcher() (dispatch.Dispatcher, error) {
	if dispatcher != nil {
		return dispatcher, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	return dispatch.NewShellDispatcher(cfg.AgentMuxBin), nil
}

func cmdDispatch(args []string) error {
	baseDir, args, err := resolveBaseDir(args)
	if err != nil {
		return err
	}

	id := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		id = args[0]
		args = args[1:]
	}

	fs := newFlagSet("dispatch")
	profile := fs.String("profile", "", "profile")
	engine := fs.String("engine", "", "engine")
	model := fs.String("model", "", "model")
	effort := fs.String("effort", "", "effort")
	// -1 sentinel means "not set on CLI"; 0 means "explicitly disable stagger".
	staggerFlag := fs.Int("stagger-seconds", -1, "seconds to sleep between dispatches when multiple IDs are given (0 disables; unset applies a 15s floor over config)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z] [--stagger-seconds N]")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z] [--stagger-seconds N]")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	d, err := getDispatcher()
	if err != nil {
		return err
	}

	ids := splitCSV(id)
	if len(ids) == 0 {
		return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z] [--stagger-seconds N]")
	}

	stagger, staggerSource := resolveDispatchStagger(len(ids), cfg.StaggerSeconds, *staggerFlag)
	if len(ids) > 1 && stagger > 0 {
		_, _ = fmt.Fprintf(stdout, "multi-ID dispatch: applying %ds stagger (%s)\n", stagger, staggerSource)
	}

	var failures []string
	for i, ticketID := range ids {
		result, err := dispatchTicket(baseDir, ticketID, d, cfg, dispatch.DispatchOptions{
			Profile: *profile,
			Engine:  *engine,
			Model:   *model,
			Effort:  *effort,
		})
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: error: %v\n", ticketID, err)
			failures = append(failures, fmt.Sprintf("%s: %v", ticketID, err))
		} else {
			_, _ = fmt.Fprintf(stdout, "%s: dispatched dispatch_id=%s\n", ticketID, result.DispatchID)
		}

		if stagger > 0 && i < len(ids)-1 {
			time.Sleep(time.Duration(stagger) * time.Second)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("dispatch failed for %d ticket(s): %s", len(failures), strings.Join(failures, "; "))
	}

	return nil
}

// resolveDispatchStagger picks the inter-dispatch sleep for manual multi-ID
// dispatch. Rules:
//   - idsCount <= 1  → no stagger, never
//   - flagVal == 0   → caller explicitly disabled (no floor applied)
//   - flagVal > 0    → caller-provided value wins verbatim (no floor)
//   - flagVal < 0    → unset on CLI; use cfg.StaggerSeconds with a 15s floor
//
// Returns the stagger seconds plus a human-readable source string used in
// the stdout announcement.
func resolveDispatchStagger(idsCount, cfgStagger, flagVal int) (int, string) {
	if idsCount <= 1 {
		return 0, ""
	}
	if flagVal == 0 {
		return 0, "disabled via --stagger-seconds=0"
	}
	if flagVal > 0 {
		return flagVal, "via --stagger-seconds"
	}
	// Unset: config default with multi-ID floor.
	if cfgStagger < multiIDStaggerFloor {
		return multiIDStaggerFloor, fmt.Sprintf("floor; override with --stagger-seconds or TICKETS_STAGGER_SECONDS>=%d", multiIDStaggerFloor)
	}
	return cfgStagger, "from .tickets.toml stagger_seconds"
}

func dispatchTicket(baseDir, id string, d dispatch.Dispatcher, cfg config.Config, opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return nil, err
	}
	resolvedOpts, err := resolveDispatchOptions(baseDir, doc.Card, opts, cfg)
	if err != nil {
		return nil, err
	}
	if doc.Card.Status != frontmatter.StatusOpen {
		return nil, fmt.Errorf("ticket must be open to dispatch: %s", doc.Card.Status)
	}
	for _, aw := range doc.Card.Awaits {
		_, awDoc, err := loadTicket(baseDir, aw)
		if err != nil {
			return nil, fmt.Errorf("awaited ticket %s: %w", aw, err)
		}
		if !awDoc.Card.Status.IsTerminal() {
			return nil, fmt.Errorf("awaited ticket %s is not terminal (status: %s)", aw, awDoc.Card.Status)
		}
	}
	for _, dep := range doc.Card.DependsOn {
		_, depDoc, err := loadTicket(baseDir, dep)
		if err != nil {
			return nil, fmt.Errorf("dependency %s: %w", dep, err)
		}
		if depDoc.Card.Status != frontmatter.StatusDone {
			return nil, fmt.Errorf("dependency %s is not done", dep)
		}
	}
	if strings.TrimSpace(doc.GetSection("Scope")) == "" {
		return nil, fmt.Errorf("ticket must include a non-empty ## Scope section before dispatch")
	}

	fsmResult, err := fsm.Apply(doc.Card.Status, fsm.TransitionDispatch)
	if err != nil {
		return nil, err
	}

	absPath, err := ticketAbsPath(baseDir, path)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(absPath) {
		return nil, fmt.Errorf("ticket path must be absolute: %s", absPath)
	}

	// Step 1: Dispatch to agent-mux --async. This returns immediately with
	// a dispatch_id (the worker starts in the background).
	preamble := dispatchPreamble(doc)
	dispatchResult, err := d.Dispatch(dispatch.DispatchOptions{
		Profile:       resolvedOpts.Profile,
		Engine:        resolvedOpts.Engine,
		Model:         resolvedOpts.Model,
		Effort:        resolvedOpts.Effort,
		WorkDir:       resolvedOpts.WorkDir,
		Skills:        resolvedOpts.Skills,
		TicketPath:    absPath,
		Preamble:      preamble,
		ProfileSource: resolvedOpts.ProfileSource,
		EngineSource:  resolvedOpts.EngineSource,
		ModelSource:   resolvedOpts.ModelSource,
		EffortSource:  resolvedOpts.EffortSource,
	})
	if err != nil {
		return nil, err
	}

	// Step 2: Write the card in one atomic write with status=dispatched,
	// dispatch_id, and all dispatch fields. When engine/model/effort were
	// omitted from the dispatch (profile handles them), record
	// "profile-defined" so the card doesn't lie about what engine ran.
	engineOmitted := !dispatch.ShouldPassEngineFlags(resolvedOpts)
	doc.Card.Status = fsmResult.To
	doc.Card.Profile = stringPtr(resolvedOpts.Profile)
	if engineOmitted {
		doc.Card.Engine = stringPtr(profileDefinedSentinel)
		doc.Card.Model = stringPtr(profileDefinedSentinel)
		doc.Card.Effort = nil
	} else {
		doc.Card.Engine = stringPtr(resolvedOpts.Engine)
		doc.Card.Model = stringPtr(resolvedOpts.Model)
		if resolvedOpts.Effort != "" {
			doc.Card.Effort = stringPtr(resolvedOpts.Effort)
		} else {
			doc.Card.Effort = nil
		}
	}
	doc.Card.DispatchID = &dispatchResult.DispatchID
	now := timestamp()
	doc.Card.DispatchedAt = &now
	// session_id is not in the async_started response; leave nil for
	// reconcile to backfill via agent-mux status.
	if dispatchResult.SessionID != "" {
		doc.Card.SessionID = &dispatchResult.SessionID
	}
	// Clear stale outcome from previous attempts.
	if fsmResult.ClearLastOutcome {
		doc.Card.LastAttemptOutcome = nil
	}

	logEngine := valueOrBlank(doc.Card.Engine)
	logModel := valueOrBlank(doc.Card.Model)
	logEffort := valueOrBlank(doc.Card.Effort)
	appendLog(doc, fmt.Sprintf("dispatched -- %s/%s/%s dispatch_id=%s profile=%s", logEngine, logModel, logEffort, dispatchResult.DispatchID, resolvedOpts.Profile))

	if err := doc.WriteFile(path); err != nil {
		return nil, err
	}

	return dispatchResult, nil
}

func resolveDispatchOptions(baseDir string, card frontmatter.Card, opts dispatch.DispatchOptions, cfg config.Config) (dispatch.DispatchOptions, error) {
	repoRoot, err := config.RepoRoot()
	if err != nil {
		return dispatch.DispatchOptions{}, err
	}
	skills := card.Skills
	if len(opts.Skills) > 0 {
		skills = opts.Skills
	}

	// Look up initiative-level default_profile.
	var initProfile string
	if card.Initiative != "" {
		initPath := filepath.Join(baseDir, "INITIATIVES", card.Initiative+".md")
		initDoc, err := frontmatter.ParseFile(initPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠ warning: could not read initiative %q default_profile: %v — falling back to global default\n", card.Initiative, err)
		} else {
			initProfile = valueOrBlank(initDoc.Card.DefaultProfile)
			if initProfile == "" {
				fmt.Fprintf(os.Stderr, "⚠ warning: initiative %q has no default_profile — using global default %q\n", card.Initiative, cfg.Defaults.Profile)
			}
		}
	}

	// Resolve each field through the cascade: CLI -> card -> initiative(profile only) -> config.
	// Track which source won for each field.
	profileVal, profileSrc := resolveWithSource(
		opts.Profile, valueOrBlank(card.Profile), initProfile, cfg.Defaults.Profile,
	)
	engineVal, engineSrc := resolveWithSource(
		opts.Engine, valueOrBlank(card.Engine), "", cfg.Defaults.Engine,
	)
	modelVal, modelSrc := resolveWithSource(
		opts.Model, valueOrBlank(card.Model), "", cfg.Defaults.Model,
	)
	effortVal, effortSrc := resolveWithSource(
		opts.Effort, valueOrBlank(card.Effort), "", cfg.Defaults.Effort,
	)

	resolved := dispatch.DispatchOptions{
		Profile:       profileVal,
		Engine:        engineVal,
		Model:         modelVal,
		Effort:        effortVal,
		WorkDir:       repoRoot,
		Skills:        skills,
		ProfileSource: profileSrc,
		EngineSource:  engineSrc,
		ModelSource:   modelSrc,
		EffortSource:  effortSrc,
	}
	if resolved.Profile == "" {
		return dispatch.DispatchOptions{}, fmt.Errorf("dispatch requires profile via --profile, ticket frontmatter, or .tickets.toml defaults")
	}
	if resolved.Engine == "" || resolved.Model == "" {
		return dispatch.DispatchOptions{}, fmt.Errorf("dispatch requires engine and model via flags, ticket frontmatter, or .tickets.toml defaults")
	}
	return resolved, nil
}

// profileDefinedSentinel is written to card frontmatter when engine/model/effort
// are omitted from dispatch and left to the profile. On re-dispatch it must be
// treated as empty so the resolve cascade falls through to config defaults
// instead of passing the literal sentinel to agent-mux.
const profileDefinedSentinel = "profile-defined"

// resolveWithSource picks the first non-empty value from the cascade and
// returns which source it came from:
//   cli (flag) -> card (frontmatter) -> initiative -> config (global default)
func resolveWithSource(cli, card, initiative, cfg string) (string, dispatch.OptionSource) {
	if strings.TrimSpace(cli) != "" {
		return cli, dispatch.SourceCLI
	}
	if v := strings.TrimSpace(card); v != "" && v != profileDefinedSentinel {
		return card, dispatch.SourceCard
	}
	if strings.TrimSpace(initiative) != "" {
		return initiative, dispatch.SourceInitiative
	}
	if strings.TrimSpace(cfg) != "" {
		return cfg, dispatch.SourceConfig
	}
	return "", dispatch.SourceNone
}

func dispatchPreamble(doc *frontmatter.Document) string {
	if doc.Card.Attempts <= 0 {
		return ""
	}

	return fmt.Sprintf(
		"This ticket has had %d previous attempt(s).\nLast outcome: %s\nPartial result may exist in ## Result. Check ## Log for details.\nAdjust your approach accordingly.",
		doc.Card.Attempts,
		valueOrBlank(doc.Card.LastAttemptOutcome),
	)
}

func stringPtr(s string) *string {
	return &s
}

func valueOrBlank(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
