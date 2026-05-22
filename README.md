# agent-tickets

I wanted a ticket system that did not become another place to manage work about work.

The itch was simple: AI agents are useful once the work unit is clear, bounded, and recoverable. A chat message is too soft. A full project-management app is too much furniture. So `agent-tickets` keeps the thing in the boring place that already works: markdown files in your repo, with YAML frontmatter for the parts machines need to trust.

Cards are the database. Git history is the audit trail. The state machine is the guardrail.

## What It Is

`agent-tickets` is a Go CLI for filesystem-native agent work:

- Markdown ticket cards with YAML frontmatter
- Initiative folders for grouping related work
- A finite-state lifecycle: `open`, `dispatched`, `done`, `failed`, `blocked`, `closed`
- Hard dependencies with `depends_on`
- Soft completion gates with `awaits`
- Optional `agent-mux` dispatch and reconciliation
- Offline-friendly create/list/board workflows

No server. No database. No queue service hiding in the corner.

## Install

You need Go. The module currently targets Go `1.24.2`.

Install the latest released CLI:

```bash
go install github.com/buildoak/agent-tickets/cmd/tickets@latest
```

Or build from a local checkout:

```bash
git clone https://github.com/buildoak/agent-tickets.git
cd agent-tickets
go build -o ./tickets ./cmd/tickets
```

If you build locally, either run `./tickets ...` from the checkout or move the binary somewhere on your `PATH`.

## Quickstart

This quickstart does not require `agent-mux`. It creates a local ticket tree under `./tickets`.

```bash
mkdir ticket-demo
cd ticket-demo

cat > .tickets.toml <<'TOML'
base_dir = "tickets"
max_retry = 3

[stall_timeout_minutes]
worker = 30
deep = 45
heavy = 60
TOML

tickets init DEMO --title "Demo work"
tickets create --initiative DEMO --title "Write the first scoped card" --tier worker
tickets board
tickets show DEMO-001
```

Now edit `tickets/cards/DEMO/DEMO-001.md` and fill in `## Scope`.

```bash
tickets list --initiative DEMO --status open
```

That is the offline loop: create cards, inspect them, move them through explicit states when needed, and keep everything reviewable in git.

## Dispatch Is Optional

`agent-mux` is only needed when you want tickets to launch workers or reconcile worker results.

Without `agent-mux`, these workflows still work:

- `init`
- `create`
- `show`
- `list`
- `board`
- `summary`
- `initiatives`
- manual lifecycle commands such as `block`, `close`, and `reopen`

With `agent-mux` installed and configured, `tickets dispatch`, `tickets dispatch-ready`, `tickets reconcile`, and `tickets tick` can hand eligible cards to async workers and pull their status back into the markdown cards.

Tests use a mock dispatcher, so the core CLI behavior can be verified without a live runtime.

## The Card

A ticket is a markdown file with structured frontmatter and human-readable sections.

```markdown
---
id: DEMO-001
initiative: DEMO
title: Write the first scoped card
status: open
tier: worker
tags: []
created: "2026-05-22"
manual: false
depends_on: []
awaits: []
skills: []
attempts: 0
---

## Context

What a zero-context worker needs to know.

## Scope

The deliverable and the definition of done.

## Result

Filled by the worker or by the human closing the loop.

## Log

open -- created
```

The important bit is not the YAML per se. It is that humans and agents are looking at the same object. No hidden database state, no separate dashboard truth.

## Lifecycle

```text
open ──dispatch──> dispatched ──complete──> done
  │                    │                     │
  │                    ├──fail────> failed    ├──reopen──> open
  │                    │             │        └──close───> closed
  │                    └──cancel──> open
  │
  ├──block──> blocked ──reopen──> open
  │
  └──close──> closed

failed ──reopen──> open
       ──block───> blocked
       ──close───> closed
```

`depends_on` is strict: every dependency must be `done`.

`awaits` is softer: every awaited ticket must be terminal, meaning `done`, `failed`, `blocked`, or `closed`. This is useful for audit batches, cleanup passes, or reports where completion matters more than success.

After `max_retry` consecutive failures, a ticket auto-blocks instead of looping forever. Quiet guardrail. Properly useful.

## Command Map

Lifecycle:

| Command | What it does |
| --- | --- |
| `init` | Create an initiative and its card directory |
| `create` | Create a ticket under an initiative |
| `dispatch` | Send open ticket(s) to `agent-mux` |
| `complete` | Mark a dispatched ticket as done |
| `fail` | Mark a dispatched ticket as failed |
| `cancel` | Return a dispatched ticket to open |
| `reopen` | Reopen a failed, done, or blocked ticket |
| `block` | Mark a ticket blocked with a reason |
| `close` | Permanently close a ticket |

Automation:

| Command | What it does |
| --- | --- |
| `dispatch-ready` | Dispatch eligible open tickets |
| `reconcile` | Sync dispatched tickets from `agent-mux` status |
| `tick` | Run one automation cycle: reconcile, stall detection, dispatch-ready |

Queries:

| Command | What it does |
| --- | --- |
| `show` | Print one card as markdown or JSON |
| `list` | List cards with filters |
| `board` | Show a kanban-style board |
| `summary` | Print compact status counts by initiative |
| `initiatives` | List initiatives and ticket counts |

Maintenance:

| Command | What it does |
| --- | --- |
| `edit` | Open a card in `$EDITOR` and validate it on save |
| `delete` | Delete a ticket, optionally cascading dependents |
| `migrate` | Move a ticket to another initiative and rewrite dependencies |

Run `tickets <command> --help` for command-specific flags.

## Configuration

`tickets` looks for `.tickets.toml` in the current directory or any ancestor.

For public or team use, set `base_dir` explicitly:

```toml
base_dir = "tickets"
agent_mux_bin = "agent-mux"
max_retry = 3
stagger_seconds = 2
max_dispatch_per_tick = 1
skill_path = ""

[defaults]
profile = "ticket-worker"
engine = "codex"
model = "your-model-name"
effort = "high"

[stall_timeout_minutes]
worker = 30
deep = 45
heavy = 60

[concurrency]
codex = 3
claude = 2

[model_weight]
your-model-name = 1

[profile_engine]
ticket-worker = "codex"

[profile_model]
ticket-worker = "your-model-name"

[initiatives.DEMO]
stall_timeout_minutes = 45
```

Environment overrides:

- `TICKETS_BASE_DIR`
- `TICKETS_AGENT_MUX_BIN`
- `TICKETS_STAGGER_SECONDS`

Resolution rules for dispatch:

- `profile`: CLI flag -> card frontmatter -> initiative `default_profile` -> `.tickets.toml` defaults
- `skills`: CLI `--skills` -> initiative `default_skills` -> empty
- `engine`, `model`, `effort`: CLI flag -> card frontmatter -> `.tickets.toml` defaults

Initiative defaults live in `tickets/INITIATIVES/<NAME>.md` frontmatter, not in the config file.

## Automation Notes

`tick` is designed to be cheap enough for repeated runs:

- It uses a file lock so overlapping automation cycles do not stampede the tree.
- It skips work when the cards tree has not changed and the stall-check window has not elapsed.
- When it does run, it parses cards once and shares that slice across reconcile, stall detection, and dispatch-ready.
- Stall detection warns with `[STALL_WARNING]` and auto-fails timed-out dispatched tickets.

Reconcile is intentionally conservative. It only asks `agent-mux` about cards in `dispatched`. Terminal cards are left alone.

One practical detail I care about: if `agent-mux` reports failure but the card has a populated `## Result`, reconcile trusts the artifact and marks the ticket done. Generated status is not more real than the work sitting in the card.

## Architecture

```text
cmd/tickets/     CLI commands, one file per command
frontmatter/     YAML frontmatter parser with byte-exact round-trip preservation
fsm/             State machine and lifecycle transition rules
dispatch/        Dispatcher interface, shell adapter for agent-mux, mock for tests
config/          TOML loading, defaults, timeouts, profile/model accounting
```

Design constraints:

- Cards are the source of truth.
- State changes go through the FSM.
- Commands use the dispatcher interface, never `agent-mux` directly.
- Frontmatter edits preserve untouched bytes.
- Dependencies stay small: YAML and TOML parsing, nothing heavier.

## Development

```bash
go test ./...
go build -o ./tickets ./cmd/tickets
```

Tests are integration-style around the CLI command paths, plus focused package tests for frontmatter, dispatch, and the FSM.

## Why I Like This Shape

The point is not to recreate Jira in markdown. The point is to give agents a crisp work object and give humans a thing they can review, diff, retry, archive, and trust.

Small enough to understand. Structured enough to automate. Boring enough to keep using after the initial enthusiasm wears off.

P.S. The nice part is that the ticket file can be pasted directly into an agent as the prompt. That was not an accident.
