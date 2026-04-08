package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/buildoak/agent-tickets/config"
	"github.com/buildoak/agent-tickets/dispatch"
	"github.com/buildoak/agent-tickets/frontmatter"
	"github.com/buildoak/agent-tickets/fsm"
)

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
	fs.SetOutput(stderr)
	profile := fs.String("profile", "", "profile")
	engine := fs.String("engine", "", "engine")
	model := fs.String("model", "", "model")
	effort := fs.String("effort", "", "effort")
	cwd := fs.String("cwd", "", "working directory for agent-mux dispatch")
	var skills stringSliceFlag
	fs.Var(&skills, "skill", "skill name to pass to agent-mux (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if id == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z --cwd DIR --skill S]")
		}
		id = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z --cwd DIR --skill S]")
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
		return fmt.Errorf("usage: tickets dispatch TICKET-ID[,TICKET-ID...] [--profile P --engine X --model Y --effort Z]")
	}

	var failures []string
	for _, ticketID := range ids {
		result, err := dispatchTicket(baseDir, ticketID, d, cfg, dispatch.DispatchOptions{
			Profile: *profile,
			Engine:  *engine,
			Model:   *model,
			Effort:  *effort,
			WorkDir: *cwd,
			Skills:  []string(skills),
		})
		if err != nil {
			_, _ = fmt.Fprintf(stdout, "%s: error: %v\n", ticketID, err)
			failures = append(failures, fmt.Sprintf("%s: %v", ticketID, err))
			continue
		}

		_, _ = fmt.Fprintf(stdout, "%s: dispatched dispatch_id=%s session_id=%s\n", ticketID, result.DispatchID, result.SessionID)
	}

	if len(failures) > 0 {
		return fmt.Errorf("dispatch failed for %d ticket(s): %s", len(failures), strings.Join(failures, "; "))
	}

	return nil
}

func dispatchTicket(baseDir, id string, d dispatch.Dispatcher, cfg config.Config, opts dispatch.DispatchOptions) (*dispatch.DispatchResult, error) {
	path, doc, err := loadTicket(baseDir, id)
	if err != nil {
		return nil, err
	}
	resolvedOpts, err := resolveDispatchOptions(doc.Card, opts, cfg)
	if err != nil {
		return nil, err
	}
	if doc.Card.Status != frontmatter.StatusOpen {
		return nil, fmt.Errorf("ticket must be open to dispatch: %s", doc.Card.Status)
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

	result, err := fsm.Apply(doc.Card.Status, fsm.TransitionDispatch)
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

	dispatchResult, err := d.Dispatch(dispatch.DispatchOptions{
		Profile:    resolvedOpts.Profile,
		Engine:     resolvedOpts.Engine,
		Model:      resolvedOpts.Model,
		Effort:     resolvedOpts.Effort,
		WorkDir:    resolvedOpts.WorkDir,
		TicketPath: absPath,
		Preamble:   dispatchPreamble(doc),
		Skills:     resolvedOpts.Skills,
	})
	if err != nil {
		return nil, err
	}

	doc.Card.Status = result.To
	doc.Card.Profile = stringPtr(resolvedOpts.Profile)
	doc.Card.Engine = stringPtr(resolvedOpts.Engine)
	doc.Card.Model = stringPtr(resolvedOpts.Model)
	if resolvedOpts.Effort != "" {
		doc.Card.Effort = stringPtr(resolvedOpts.Effort)
	} else {
		doc.Card.Effort = nil
	}
	doc.Card.DispatchID = &dispatchResult.DispatchID
	doc.Card.SessionID = &dispatchResult.SessionID

	appendLog(doc, fmt.Sprintf("dispatched -- %s/%s/%s dispatch_id=%s session_id=%s profile=%s", valueOrBlank(doc.Card.Engine), valueOrBlank(doc.Card.Model), valueOrBlank(doc.Card.Effort), dispatchResult.DispatchID, dispatchResult.SessionID, resolvedOpts.Profile))

	if err := doc.WriteFile(path); err != nil {
		return nil, err
	}

	return dispatchResult, nil
}

func resolveDispatchOptions(card frontmatter.Card, opts dispatch.DispatchOptions, cfg config.Config) (dispatch.DispatchOptions, error) {
	resolved := dispatch.DispatchOptions{
		Profile: firstNonEmpty(opts.Profile, valueOrBlank(card.Profile), cfg.Defaults.Profile),
		Engine:  firstNonEmpty(opts.Engine, valueOrBlank(card.Engine), cfg.Defaults.Engine),
		Model:   firstNonEmpty(opts.Model, valueOrBlank(card.Model), cfg.Defaults.Model),
		Effort:  firstNonEmpty(opts.Effort, valueOrBlank(card.Effort), cfg.Defaults.Effort),
		WorkDir: firstNonEmpty(opts.WorkDir, valueOrBlank(card.WorkDir), cfg.Defaults.WorkDir),
	}

	// Skills are additive: config defaults + card frontmatter + CLI flags.
	var skills []string
	skills = append(skills, cfg.Defaults.Skills...)
	skills = append(skills, card.Skills...)
	skills = append(skills, opts.Skills...)
	resolved.Skills = skills

	if resolved.Profile == "" {
		return dispatch.DispatchOptions{}, fmt.Errorf("dispatch requires profile via --profile, ticket frontmatter, or .tickets.toml defaults")
	}
	if resolved.Engine == "" || resolved.Model == "" {
		return dispatch.DispatchOptions{}, fmt.Errorf("dispatch requires engine and model via flags, ticket frontmatter, or .tickets.toml defaults")
	}
	return resolved, nil
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
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

// stringSliceFlag implements flag.Value for repeatable string flags.
type stringSliceFlag []string

func (f *stringSliceFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
