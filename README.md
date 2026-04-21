# agent-tickets

Filesystem-native ticket system for AI agent workflows. Markdown cards with YAML frontmatter, FSM-enforced lifecycle, agent-mux dispatch.

No database. Cards are the source of truth. Git history is the audit trail.

## Why

AI agents need structured work units between "just do it" and full project management. agent-tickets gives you:

- **Markdown cards** — human-readable, git-tracked, zero infrastructure
- **FSM lifecycle** — tickets move through validated state transitions, not arbitrary status strings
- **Agent-mux dispatch** — fire-and-forget worker dispatch with automatic reconciliation
- **Initiative routing** — group tickets by domain with per-initiative defaults and worker onboarding

## Install

```bash
go build -o ~/.local/bin/tickets ./cmd/tickets
```

## Quick Start

```bash
# Create an initiative
tickets init RESEARCH --title "Research tasks"

# Create a ticket
tickets create --initiative RESEARCH --title "Survey compression techniques" --tier worker

# Edit the card — write Context and Scope sections
tickets edit RESEARCH-001

# Dispatch to a worker
tickets dispatch RESEARCH-001

# Check status
tickets summary
tickets board --status dispatched

# Reconcile with agent-mux (sync worker results)
tickets reconcile
```

## Card Format

```markdown
---
id: RESEARCH-001
initiative: RESEARCH
title: Survey compression techniques
status: open
tier: worker
tags: [research, compression]
created: "2026-04-12"
skills: [web-search]
depends_on: []
awaits: []          # soft dependencies (terminal-state gating)
---

## Context
Brief onboarding for a zero-context worker.
What files to read, what background to know.

## Scope
What to deliver + definition of done.
Not step-by-step instructions — the worker decides how.

## Result
[filled by worker]

## Log
[operational history — archived results, retry notes]
```

Optional frontmatter fields used by the runtime include `plan_ref`, `profile`, `dispatch_id`, `session_id`, `dispatched_at`, `last_attempt_outcome`, and `block_reason`.

### Dependencies

- `depends_on: [A, B]` — hard gate. All listed tickets must reach `done` before dispatch.
- `awaits: [A, B]` — soft gate. All listed tickets must reach any terminal state (`done`, `failed`, `blocked`, `closed`).

Use `depends_on` when you need upstream output. Use `awaits` when you need upstream completion regardless of outcome (audits, reports, cleanup). Both fields can coexist; both must be satisfied.

## State Machine

```
open ──dispatch──> dispatched ──complete──> done
  │                    │                     │
  │                    ├──fail────> failed    ├──reopen──> open
  │                    │             │        └──close───> closed
  │                    └──cancel──> open
  │                                  │
  ├──block──> blocked ──reopen──> open
  │
  └──close──> closed

failed ──reopen──> open
       ──block───> blocked
       ──close───> closed
```

**Six states:** `open`, `dispatched`, `done`, `failed`, `blocked`, `closed`

| State | Meaning |
|-------|---------|
| `open` | Ready for work or dispatch |
| `dispatched` | Worker is running |
| `done` | Worker completed successfully |
| `failed` | Worker failed (retryable via reopen) |
| `blocked` | Stuck on external dependency or needs human input |
| `closed` | Conceptually dead — never retry (distinct from failed) |

**Terminal states** (for `awaits` gating): `done`, `failed`, `blocked`, `closed`. **Non-terminal:** `open`, `dispatched`.

Auto-block: after `max_retry` (default 3) consecutive failures, ticket auto-blocks.

## CLI Reference

### Lifecycle

| Command | Description |
|---------|-------------|
| `create` | Create a new ticket card under an initiative (`--awaits A,B` for soft deps) |
| `dispatch` | Dispatch ticket(s) to agent-mux workers |
| `complete` | Mark dispatched ticket as done (called by worker) |
| `fail` | Mark dispatched ticket as failed with reason (called by worker) |
| `cancel` | Cancel dispatched ticket, return to open |
| `reopen` | Reopen failed/done/blocked ticket for retry |
| `block` | Block ticket with a reason |
| `close` | Permanently close ticket (conceptual death) |

### Automation

| Command | Description |
|---------|-------------|
| `tick` | One automation cycle: reconcile + stall-detect + dispatch-ready (with dir-mtime fast-path) |
| `reconcile` | Sync dispatched tickets with agent-mux status, trusting `## Result` over raw exit status when they disagree |
| `dispatch-ready` | Auto-dispatch eligible open tickets (hard + soft deps met, scope filled) |

`tick` is cheap when the tree is idle. It stats the `cards/` tree mtime on wake and, if nothing changed since the persisted `.tick-state` cursor, exits with `tick: no-change skip`. A 9-minute stall-check cursor (independent of dir mtime) ensures slow workers don't hide behind an idle filesystem. When phases do run, cards are parsed exactly once and the slice is shared across reconcile/stall/dispatch-ready. Phases also early-exit when their precondition isn't met (no dispatched cards → skip reconcile; no open-ready cards → skip dispatch-ready).

Stall detection runs inside `tick`: dispatched tickets that exceed their timeout are warned with `[STALL_WARNING]` and auto-failed. Timeouts come from per-tier defaults, can be overridden per initiative in config, and fall back to the last `dispatched --` log timestamp if `dispatched_at` is missing.

### Queries

| Command | Description |
|---------|-------------|
| `show` | Display a single ticket (raw or JSON) |
| `list` | List tickets with filters |
| `board` | Kanban-style board view (shows `(awaits)` suffix for unresolved soft deps; `done` annotation is blank) |
| `summary` | Status counts by initiative (agent-friendly, ~100 tokens) |
| `initiatives` | List all initiatives with ticket counts |

### Maintenance

| Command | Description |
|---------|-------------|
| `init` | Create a new initiative with directory structure |
| `edit` | Edit ticket in $EDITOR with validation on save |
| `delete` | Delete ticket (--cascade for dependents) |
| `migrate` | Move ticket to different initiative, rewrite deps |

All commands support `--help`. Run `tickets help <command>` for detailed usage.

## Configuration

`.tickets.toml` at repo root:

```toml
base_dir = "centerpiece/tickets"
agent_mux_bin = "agent-mux"
max_retry = 3
stagger_seconds = 2
max_dispatch_per_tick = 1
skill_path = ""

[defaults]
engine = "codex"
model = "gpt-5.4-mini"
effort = "xhigh"
profile = "jenkins-junior"

[stall_timeout_minutes]
worker = 30
deep = 45
heavy = 60

[concurrency]
codex = 5
claude = 3
gemini = 2

[model_weight]
"gpt-5.4-mini" = 1

[profile_engine]
jenkins-junior = "codex"

[profile_model]
jenkins-junior = "gpt-5.4-mini"

# Per-initiative stall override. default_profile and default_skills are set in
# INITIATIVES/<NAME>.md frontmatter, not in .tickets.toml.
[initiatives.PAPER-OPS]
stall_timeout_minutes = 90

[guardian]
engine = "codex"
model = "gpt-5.4-mini"
effort = "high"
profile = "jenkins-guardian"
initiative = "OPS"
```

Dispatch resolution:
- `profile`: CLI flag -> card frontmatter -> initiative markdown `default_profile` -> `.tickets.toml` defaults
- `skills`: CLI `--skills` -> initiative markdown `default_skills` -> empty
- `engine`, `model`, `effort`: CLI flag -> card frontmatter -> `.tickets.toml` defaults

Config notes:
- `skill_path` is loaded from config to carry a custom skill path setting.
- `model_weight` and `[concurrency]` together control dispatch-ready engine weight caps.
- `profile_engine` and `profile_model` map profile names to their effective engine/model for cap accounting when cards store `profile-defined`.
- `initiatives.<NAME>.stall_timeout_minutes` overrides stall detection timeout for one initiative.
- `[guardian]` enables guardian mode only when its required fields are fully populated.

## Architecture

```
cmd/tickets/     CLI — 20 commands, each in its own file. Router in main.go.
frontmatter/     YAML frontmatter parser. Byte-exact round-trip preservation.
fsm/             State machine. Single source of truth for lifecycle rules.
dispatch/        Dispatcher interface. Shell (agent-mux) + mock for tests. Shell dispatch reads agent-mux --async JSON until kind=async_started, ignoring preview output.
config/          TOML config. Loads .tickets.toml, applies layered defaults, stall timeouts, profile engine/model maps, and guardian settings.
```

## Dependencies

Minimal by design:

- `gopkg.in/yaml.v3` — YAML frontmatter parsing
- `github.com/BurntSushi/toml` — config file parsing

No frameworks. No HTTP. No database drivers.
