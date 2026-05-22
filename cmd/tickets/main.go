package main

import (
	"fmt"
	"io"
	"os"
)

var (
	stdout io.Writer = os.Stdout
	stderr io.Writer = os.Stderr
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		if len(args) > 1 {
			printSubcommandHelp(args[1])
		} else {
			printUsage()
		}
		return nil
	case "init":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("init")
			return nil
		}
		return cmdInit(args[1:])
	case "create":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("create")
			return nil
		}
		return cmdCreate(args[1:])
	case "show":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("show")
			return nil
		}
		return cmdShow(args[1:])
	case "list":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("list")
			return nil
		}
		return cmdList(args[1:])
	case "board":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("board")
			return nil
		}
		return cmdBoard(args[1:])
	case "initiatives":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("initiatives")
			return nil
		}
		return cmdInitiatives(args[1:])
	case "dispatch":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("dispatch")
			return nil
		}
		return cmdDispatch(args[1:])
	case "reconcile":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("reconcile")
			return nil
		}
		return cmdReconcile(args[1:])
	case "dispatch-ready":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("dispatch-ready")
			return nil
		}
		return cmdDispatchReady(args[1:])
	case "tick":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("tick")
			return nil
		}
		return cmdTick(args[1:])
	case "complete":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("complete")
			return nil
		}
		return cmdComplete(args[1:])
	case "fail":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("fail")
			return nil
		}
		return cmdFail(args[1:])
	case "cancel":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("cancel")
			return nil
		}
		return cmdCancel(args[1:])
	case "reopen":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("reopen")
			return nil
		}
		return cmdReopen(args[1:])
	case "close":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("close")
			return nil
		}
		return cmdClose(args[1:])
	case "block":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("block")
			return nil
		}
		return cmdBlock(args[1:])
	case "edit":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("edit")
			return nil
		}
		return cmdEdit(args[1:])
	case "delete":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("delete")
			return nil
		}
		return cmdDelete(args[1:])
	case "migrate":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("migrate")
			return nil
		}
		return cmdMigrate(args[1:])
	case "summary":
		if hasHelpFlag(args[1:]) {
			printSubcommandHelp("summary")
			return nil
		}
		return cmdSummary(args[1:])
	default:
		return fmt.Errorf("unknown command: %s\nRun 'tickets --help' for usage.", args[0])
	}
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Fprint(stdout, `tickets — agentic ticket dispatch and lifecycle manager

Manages ticket cards stored as markdown files with YAML frontmatter.
Tickets belong to initiatives, flow through a state machine
(open -> dispatched -> done/failed/blocked/closed), and are dispatched
to AI worker agents via agent-mux.

Usage:
  tickets <command> [arguments]

Commands:
  Lifecycle:
    init             Create a new initiative with its directory structure
    create           Create a new ticket card under an initiative
    dispatch         Dispatch one or more tickets to agent-mux workers
    complete         Mark a dispatched ticket as done (worker calls this)
    fail             Mark a dispatched ticket as failed with a reason
    cancel           Cancel a dispatched ticket and reopen it
    reopen           Reopen a failed/done/blocked ticket for another attempt
    block            Block an open or failed ticket with a reason
    close            Permanently close a ticket (conceptual failure, never retry)

  Automation:
    dispatch-ready   Auto-dispatch all eligible open tickets (deps resolved, scope filled)
    tick             Run one automation cycle: reconcile + stall-detect + dispatch-ready
    reconcile        Sync dispatched ticket states with agent-mux status

  Queries:
    show             Display a single ticket card (raw markdown or JSON)
    list             List tickets with optional filters
    board            Kanban-style board view of all tickets
    initiatives      List all initiatives with ticket counts
    summary          Compact status-count table by initiative (agent-friendly)

  Maintenance:
    edit             Open a ticket in $EDITOR, validate deps on save
    delete           Delete a ticket (with optional --cascade for dependents)
    migrate          Move a ticket to a different initiative, rewriting deps

Global flags:
  --base DIR       Override the tickets base directory (default: from .tickets.toml)

Configuration:
  Reads .tickets.toml from the current directory or any ancestor.
  Environment overrides: TICKETS_BASE_DIR, TICKETS_AGENT_MUX_BIN, TICKETS_STAGGER_SECONDS.

Run 'tickets <command> --help' for details on a specific command.
`)
}

func printSubcommandHelp(cmd string) {
	help, ok := subcommandHelp[cmd]
	if !ok {
		fmt.Fprintf(stdout, "Unknown command: %s\nRun 'tickets --help' for usage.\n", cmd)
		return
	}
	fmt.Fprint(stdout, help)
}

var subcommandHelp = map[string]string{
	"init": `tickets init — create a new initiative

Usage:
  tickets init INITIATIVE --title "Initiative title"

Arguments:
  INITIATIVE       Initiative identifier (e.g. RESEARCH, BUILD). Used as directory name.

Flags:
  --title STRING   (required) Human-readable title for the initiative
  --base DIR       Override tickets base directory

Creates:
  - INITIATIVES/<INITIATIVE>.md with frontmatter (status: active)
  - cards/<INITIATIVE>/ directory for ticket files

Example:
  tickets init RESEARCH --title "Paper processing pipeline"
`,

	"create": `tickets create — create a new ticket card

Usage:
  tickets create --initiative INIT --title "..." --tier TIER [flags]

Flags:
  --initiative STRING   (required) Initiative this ticket belongs to (must exist)
  --title STRING        (required) Ticket title / one-line description
  --tier STRING         (required) Execution tier: worker, deep, or heavy
  --manual              Mark as manual (excluded from auto-dispatch)
  --depends-on IDS      Comma-separated ticket IDs this depends on (e.g. "BUILD-001,BUILD-002")
  --awaits IDS          Comma-separated ticket IDs to soft-wait on (dispatches once all are terminal)
  --skills SKILLS       Comma-separated skill names for the worker
  --base DIR            Override tickets base directory

Tier descriptions:
  worker    Standard agent task, single worker, short-lived
  deep      Extended reasoning task, may need more context/time
  heavy     Multi-step or coordinator-level task

Output:
  Prints the new ticket ID to stdout (e.g. "RESEARCH-003").

Example:
  tickets create --initiative RESEARCH --title "Extract methods from paper" --tier worker
  tickets create --initiative BUILD --title "Deep analysis" --tier deep --depends-on BUILD-001
  tickets create --initiative AUDIT --title "Audit batch" --tier worker --awaits RESEARCH-010,RESEARCH-011
`,

	"show": `tickets show — display a single ticket

Usage:
  tickets show TICKET-ID [flags]

Arguments:
  TICKET-ID       Ticket identifier (e.g. RESEARCH-003)

Flags:
  --json          Output as JSON with board annotations (status, detail)
  --base DIR      Override tickets base directory

Output:
  Default: raw markdown (frontmatter + body) to stdout.
  With --json: JSON object with card fields, annotation, and detail.

Example:
  tickets show RESEARCH-003
  tickets show RESEARCH-003 --json
`,

	"list": `tickets list — list tickets with filters

Usage:
  tickets list [flags]

Flags:
  --initiative STRING   Filter by initiative name
  --status STRING       Filter by status (open, dispatched, done, failed, blocked, closed)
  --tag STRING          Filter by tag
  --json                Output as JSON array with board annotations
  --base DIR            Override tickets base directory

Output:
  Default: table with columns ID, TITLE, TIER, STATUS.
  With --json: JSON array of card objects with annotations.

Example:
  tickets list
  tickets list --initiative RESEARCH --status open
  tickets list --status dispatched --json
`,

	"board": `tickets board — kanban-style board view

Usage:
  tickets board [flags]

Flags:
  --initiative STRING   Filter by initiative name
  --status STRING       Filter by status (open, dispatched, done, failed, blocked, closed)
  --json                Output as JSON array with board annotations
  --base DIR            Override tickets base directory

Output:
  Default: table showing ID, TITLE, TIER, and status annotation.
  Annotations include: queued, waiting (with blockers and awaits), running (with engine/model),
  done, failed (with attempt count and reason), blocked, manual,
  CLOSED (with closure reason). Unresolved soft deps show an "(awaits)" suffix.
  With --json: full card objects with annotation and detail fields.

Example:
  tickets board
  tickets board --initiative RESEARCH
  tickets board --json
`,

	"initiatives": `tickets initiatives — list all initiatives

Usage:
  tickets initiatives [flags]

Flags:
  --status STRING   Filter by initiative status (active, paused, complete, archived)
  --json            Output as JSON array
  --base DIR        Override tickets base directory

Output:
  Default: table with columns INITIATIVE, TITLE, STATUS, TICKETS (status breakdown).
  With --json: JSON array of initiative objects with ticket counts.

Example:
  tickets initiatives
  tickets initiatives --status active
  tickets initiatives --json
`,

	"dispatch": `tickets dispatch — dispatch tickets to agent-mux workers

Usage:
  tickets dispatch TICKET-ID[,TICKET-ID,...] [flags]

Arguments:
  TICKET-ID       One or more ticket IDs, comma-separated. Each must be in 'open' status
                  with a non-empty ## Scope section, all depends_on resolved (done),
                  and all awaits resolved (terminal: done/failed/blocked/closed).

Flags:
  --profile STRING         Worker profile override (default: card -> initiative default_profile -> .tickets.toml)
  --engine STRING          Engine override: codex, claude, gemini (default: card -> .tickets.toml)
  --model STRING           Model override (default: card -> .tickets.toml)
  --effort STRING          Effort level override (default: card -> .tickets.toml)
  --stagger-seconds INT    Inter-dispatch delay when N>1 IDs are given (default: max(.tickets.toml stagger_seconds, 1); 0 disables; explicit >0 has no floor)
  --base DIR               Override tickets base directory

Resolution cascade for profile/engine/model/effort:
  1. --flag (explicit CLI override)
  2. Card frontmatter (profile, engine, model, effort fields)
  3. Initiative default_profile (from INITIATIVES/<name>.md frontmatter)
  4. .tickets.toml [defaults] section

Multi-ID stagger:
  When more than one ticket ID is passed, dispatches are serialized with a
  small sleep between them (1s floor by default). Historical context: the
  original 15s floor existed because agent-mux --async did not daemonize
  and attached children were SIGKILL'd on parent exit. That was fixed in
  agent-mux v3.4.1 (commit c37febe). The 1s floor is kept as light
  protection against Codex/OpenAI rate-limit spikes on large batches; set
  --stagger-seconds=0 to disable entirely. Solo dispatch never sleeps.

Effects:
  - Calls agent-mux --async to start a background worker
  - Transitions ticket status: open -> dispatched
  - Writes dispatch_id, dispatched_at, profile, engine, model to card frontmatter
  - Appends dispatch event to ## Log section

Example:
  tickets dispatch RESEARCH-003
  tickets dispatch BUILD-001,BUILD-002 --engine claude --model opus-4
  tickets dispatch RESEARCH-005 --profile ticket-worker-heavy
  tickets dispatch A-001,B-002,C-003 --stagger-seconds 30   # explicit override
  tickets dispatch A-001,B-002 --stagger-seconds 0          # disable stagger (advanced)
`,

	"dispatch-ready": `tickets dispatch-ready — auto-dispatch eligible tickets

Usage:
  tickets dispatch-ready [flags]

Flags:
  --max INT        Maximum tickets to dispatch in this batch (default: 5)
  --dry-run        Preview which tickets would be dispatched without dispatching
  --base DIR       Override tickets base directory

Eligibility criteria:
  - Status is 'open'
  - Not marked as manual
  - Has a non-empty ## Scope section
  - All depends_on tickets are in 'done' status
  - All awaits tickets are in a terminal state (done/failed/blocked/closed)

Dispatch order: by created date (oldest first), then by ID.

Engine weight caps (from .tickets.toml [concurrency]):
  Respects per-engine weight limits. Skips tickets if dispatching would exceed
  the engine's weight cap based on currently-dispatched tickets.

Stagger:
  If stagger_seconds is configured, waits between dispatches to avoid bursts.

Example:
  tickets dispatch-ready
  tickets dispatch-ready --max 3
  tickets dispatch-ready --dry-run
`,

	"tick": `tickets tick — run one automation cycle

Usage:
  tickets tick [flags]

Flags:
  --max-dispatch INT   Maximum tickets to dispatch (default: from .tickets.toml max_dispatch_per_tick)
  --base DIR           Override tickets base directory

Fast-path: on wake, tick stats the cards/ tree mtime and compares to the
persisted cursor in .tick-state. If nothing has changed since last run AND
the stall-check window (9 min) hasn't elapsed, tick emits "no-change skip"
and exits in a handful of milliseconds. When phases do run, cards are
parsed once and the slice is shared across reconcile/stall/dispatch-ready.
Phases also early-exit when their precondition isn't met (no dispatched
cards → skip reconcile; no open-ready cards → skip dispatch-ready).

Runs three phases in sequence (when work is detected):
  1. Reconcile: sync dispatched ticket states with agent-mux status
  2. Stall detection: find tickets exceeding their tier's timeout, auto-fail
  3. Dispatch-ready: auto-dispatch eligible open tickets up to --max-dispatch

Uses a file lock (.tick.lock) to prevent concurrent tick runs.
Designed to be called periodically by a LaunchAgent or cron job.

Output:
  "tick: reconciled N, stalled N, dispatched N"
  "tick: no-change skip"   (fast path)

Example:
  tickets tick
  tickets tick --max-dispatch 3
`,

	"reconcile": `tickets reconcile — sync dispatched tickets with agent-mux status

Usage:
  tickets reconcile [flags]

Flags:
  --dry-run        Preview reconcile changes without applying
  --base DIR       Override tickets base directory

For each dispatched ticket, queries agent-mux status and:
  - Running: no change (backfills session_id if missing)
  - Completed: transitions to done
  - Failed: transitions to failed, records reason, auto-blocks after max_retry attempts
  - Timeout: transitions to failed with timeout reason

Terminal cards (done/failed/blocked/closed) are not re-queried.

Example:
  tickets reconcile
  tickets reconcile --dry-run
`,

	"complete": `tickets complete — mark a ticket as done

Usage:
  tickets complete TICKET-ID

Arguments:
  TICKET-ID       Ticket identifier (must be in 'dispatched' status)

Flags:
  --base DIR      Override tickets base directory

Requirements:
  - Ticket must be in 'dispatched' status
  - ## Result section must be non-empty and not placeholder text
  - The worker agent should write results to ## Result before calling this

Effects:
  - Transitions status: dispatched -> done
  - Appends completion event to ## Log

Example:
  tickets complete RESEARCH-003
`,

	"fail": `tickets fail — mark a ticket as failed

Usage:
  tickets fail TICKET-ID --reason "..."

Arguments:
  TICKET-ID       Ticket identifier (must be in 'dispatched' status)

Flags:
  --reason STRING   (required) Failure reason
  --base DIR        Override tickets base directory

Effects:
  - Transitions status: dispatched -> failed
  - Records last_attempt_outcome for retry context
  - Auto-blocks the ticket after max_retry consecutive failures (default: 3)
  - Appends failure event to ## Log

Example:
  tickets fail RESEARCH-003 --reason "PDF extraction timed out"
`,

	"cancel": `tickets cancel — cancel a dispatched ticket

Usage:
  tickets cancel TICKET-ID --reason "..."

Arguments:
  TICKET-ID       Ticket identifier (must be in 'dispatched' status)

Flags:
  --reason STRING   (required) Cancellation reason
  --base DIR        Override tickets base directory

Effects:
  - Transitions status: dispatched -> open (not failed — preserves attempt count)
  - Clears dispatch fields (dispatch_id, session_id, etc.)
  - Records last_attempt_outcome
  - Appends cancellation event to ## Log

Example:
  tickets cancel RESEARCH-003 --reason "Wrong engine selected"
`,

	"reopen": `tickets reopen — reopen a ticket for another attempt

Usage:
  tickets reopen TICKET-ID

Arguments:
  TICKET-ID       Ticket identifier (must be in failed, done, or blocked status)

Flags:
  --base DIR      Override tickets base directory

Effects:
  - Transitions status: failed/done/blocked -> open
  - Increments attempt counter
  - Clears dispatch fields and block reason
  - Archives current ## Result content to ## Log
  - Clears ## Result section for the next attempt

Example:
  tickets reopen RESEARCH-003
`,

	"close": `tickets close — permanently close a ticket

Usage:
  tickets close TICKET-ID --reason "..."

Arguments:
  TICKET-ID       Ticket identifier (must be in 'open', 'failed', or 'done' status)

Flags:
  --reason STRING   (required) Closure reason (conceptual failure, permanent)
  --base DIR        Override tickets base directory

Semantics:
  Closed = conceptual/permanent failure. The task is impossible or invalid.
  Examples: paper doesn't exist, bad citation, textbook not a paper, permanent paywall.
  Closed tickets are NEVER re-dispatched, NEVER counted for concurrency,
  NEVER stall-checked. This is a terminal state with no outgoing transitions.

Effects:
  - Transitions status: open/failed/done -> closed
  - Records reason in last_attempt_outcome
  - Clears dispatch fields
  - Archives ## Result (from done only) to ## Log
  - Appends closure event to ## Log

Example:
  tickets close RESEARCH-029 --reason "source-not-found: no DOI, no accessible source"
  tickets close RESEARCH-040 --reason "format-mismatch: textbook, not a paper"
`,

	"block": `tickets block — block a ticket

Usage:
  tickets block TICKET-ID --reason "..."

Arguments:
  TICKET-ID       Ticket identifier (must be in 'open' or 'failed' status)

Flags:
  --reason STRING   (required) Block reason
  --base DIR        Override tickets base directory

Effects:
  - Transitions status: open/failed -> blocked
  - Records block_reason in frontmatter
  - Appends block event to ## Log

Use 'tickets reopen' to unblock.

Example:
  tickets block RESEARCH-003 --reason "Waiting for API key"
`,

	"edit": `tickets edit — open a ticket in $EDITOR

Usage:
  tickets edit TICKET-ID

Arguments:
  TICKET-ID       Ticket identifier

Flags:
  --base DIR      Override tickets base directory

Opens the ticket markdown file in $EDITOR. After saving, validates
dependency graph (cycle detection, max depth 3).

Requires $EDITOR environment variable to be set.

Example:
  tickets edit RESEARCH-003
`,

	"delete": `tickets delete — delete a ticket

Usage:
  tickets delete TICKET-ID [flags]

Arguments:
  TICKET-ID       Ticket identifier (cannot be in 'dispatched' status)

Flags:
  --cascade       Also delete all tickets that depend on this one (recursively)
  --base DIR      Override tickets base directory

Without --cascade, refuses to delete if other tickets depend on this one.
With --cascade, deletes the entire dependency subtree (still refuses if any
descendant is in 'dispatched' status).

Example:
  tickets delete RESEARCH-003
  tickets delete RESEARCH-001 --cascade
`,

	"summary": `tickets summary — compact status-count table by initiative

Usage:
  tickets summary [flags]

Flags:
  --json      Output as JSON array
  --base DIR  Override tickets base directory

Output:
  Default: tabwriter table with columns Initiative, open, dispatched, done, failed, blocked, closed, total.
  Last row is TOTAL across all initiatives.
  With --json: JSON array of row objects (no TOTAL row).

Example:
  tickets summary
  tickets summary --json
`,

	"migrate": `tickets migrate — move a ticket to a different initiative

Usage:
  tickets migrate TICKET-ID TARGET-INITIATIVE [flags]

Arguments:
  TICKET-ID            Source ticket to migrate (cannot be in 'dispatched' status)
  TARGET-INITIATIVE    Destination initiative (must exist)

Flags:
  --dry-run        Preview migration without applying
  --base DIR       Override tickets base directory

Effects:
  - Assigns a new sequential ID under the target initiative
  - Rewrites depends_on and awaits references in all other tickets that pointed to the old ID
  - Validates the new dependency graph (no cycles, max depth 3)
  - Moves the ticket file to the target initiative directory
  - Refuses if the ticket or any dependent is currently dispatched

Example:
  tickets migrate BUILD-003 RESEARCH
  tickets migrate BUILD-003 RESEARCH --dry-run
`,
}
