---
name: ticket-work
description: >
  Public playbook for using agent-tickets: create markdown ticket cards,
  route work through initiatives, dispatch AI workers, monitor lifecycle
  state, and reconcile async results. Use when a task should become a
  durable ticket, dependency-aware worker dispatch, or ticket board review.
---

# Agent Tickets Coordinator Playbook

Use this skill when coordinating work with the `tickets` CLI. The system is
filesystem-native: markdown cards are the database, YAML frontmatter stores
runtime state, and initiative docs carry per-domain routing context.

Run commands from the repository that contains `.tickets.toml`, or set
`TICKETS_BASE_DIR` when operating from elsewhere. The binary is normally
available as `tickets`.

## 1. Operating Model

**Truth model:** cards are source of truth. Worker reports, dispatch status,
and board summaries are witnesses. Accept work only after checking the card
result against its scope and done gate.

**Default flow:** create a clear ticket with `manual: false`, leave it `open`,
and let `tickets tick` or `tickets dispatch-ready` dispatch eligible work. Use
`--manual` only when the user explicitly asks for controlled/manual validation
or when there is a concrete race/safety reason. Manually dispatch only when the
user explicitly asks or the workflow requires it.

**Before creating a ticket:**
- Choose an existing initiative unless the user explicitly asks for a new one.
- Read the initiative doc if it exists; it owns domain context, defaults, and
  worker onboarding.
- Decide whether the work actually needs a ticket. Trivial or interactive work
  is usually better handled directly.

**Safety gates:**
- Do not dispatch a card with an empty `## Scope`.
- Use `--dry-run` before batch dispatch, migration, or reconcile when uncertain.
- Do not delete, close, or cascade tickets casually; those actions remove future
  retry paths.
- Respect configured concurrency caps; scheduler commands enforce them better
  than ad hoc manual batches.

## 2. Files And Card Format

Common layout under the configured `base_dir`:

| Surface | Path |
|---------|------|
| Cards | `cards/{INITIATIVE}/{ID}.md` |
| Initiatives | `INITIATIVES/{NAME}.md` |
| Config | `.tickets.toml` at repo root |

Ticket IDs are `{INITIATIVE}-{seq:03d}`, for example `RESEARCH-001` or
`BUILD-014`. Sequencing is per initiative.

Cards use YAML frontmatter plus four markdown sections:

```markdown
---
id: RESEARCH-001
initiative: RESEARCH
title: Survey vector database indexing tradeoffs
status: open
tier: worker
tags: [research]
created: "2026-01-01"
manual: false
plan_ref: null
depends_on: []
awaits: []
skills: [web-search]
dispatch_id: null
session_id: null
dispatched_at: null
profile: null
engine: null
model: null
effort: null
attempts: 0
last_attempt_outcome: null
block_reason: null
---

## Context
Brief onboarding for a zero-context worker.

## Scope
What to deliver, plus a semantic definition of done.

## Result
[filled by worker]

## Log
[operational history, retry notes, archived results]
```

Not every field must be present on every card, but preserve existing
frontmatter when editing. Runtime fields such as `dispatch_id`, `session_id`,
`dispatched_at`, `last_attempt_outcome`, and `block_reason` are managed by the
CLI during lifecycle transitions.

## 3. Lifecycle FSM

| State | Transitions |
|-------|-------------|
| `open` | dispatch -> `dispatched`, block -> `blocked`, close -> `closed` |
| `dispatched` | complete -> `done`, fail -> `failed`, cancel -> `open` |
| `failed` | reopen -> `open`, block -> `blocked`, close -> `closed` |
| `blocked` | reopen -> `open` |
| `done` | reopen -> `open`, close -> `closed` |
| `closed` | terminal; no outgoing transitions |

Terminal states are `done`, `failed`, `blocked`, and `closed`. Non-terminal
states are `open` and `dispatched`.

Use `closed` for work that is conceptually dead, duplicate, or no longer worth
retrying. Use `failed` for operational failures that may be retried. After
`max_retry` consecutive failures, tickets auto-block by default.

## 4. Dependencies

| Field | Gate type | Dispatches when | Use case |
|-------|-----------|-----------------|----------|
| `depends_on` | Hard dependency | All listed tickets are `done` | Build work needs upstream output |
| `awaits` | Soft dependency | All listed tickets are terminal | Audit, cleanup, aggregation |

Both fields can coexist; both gates must clear before dispatch. Prefer
`depends_on` only when the downstream worker needs successful upstream output.
Prefer `awaits` when downstream work can proceed once upstream work has ended,
regardless of outcome.

## 5. Low-Effort Ticket Authoring

Tickets should be cheap to create and precise enough to verify. The
coordinator writes two things:

1. A succinct ask: what the worker should deliver.
2. A done gate: what must be true when the work is acceptable.

Good scope:

```markdown
## Scope
Survey current approaches to vector database filtering with approximate
nearest-neighbor search. Compare at least four approaches by recall impact,
latency impact, implementation complexity, and production maturity.

**Done means:** Result includes a compact comparison table, links to primary
sources or implementation docs, and a recommendation for a small product team.
```

Weak scope:

```markdown
## Scope
Search the web, open five tabs, summarize each page, then write a conclusion.
```

The strong version defines the artifact and acceptance gate. It does not
micromanage the worker's method.

### Context

Use `## Context` for cold-start onboarding only:
- files or docs the worker must read;
- relevant prior ticket IDs;
- domain background that is not obvious;
- working directory if it differs from repo root.

Prefer references to files over pasted content. Long context makes the card
harder to use and wastes worker attention.

### Tiers

| Tier | Use |
|------|-----|
| `worker` | Standard research, writing, audits, and implementation tasks |
| `deep` | Multi-step work that needs more reasoning or synthesis |
| `heavy` | Complex tasks needing stronger resources or longer timeouts |

Tier is not a substitute for a clear scope. It also does not inherently choose
a profile unless local configuration maps it that way.

## 6. Initiatives

Initiatives group tickets by domain and can carry defaults in their markdown
frontmatter, commonly `default_profile` and `default_skills`.

Use `templates/initiative.md` as a starter when creating a reusable workflow
contract. Use `docs/initiatives.md` when deciding whether a new initiative is
actually warranted.

Create a new initiative only when explicitly requested:

```bash
tickets init RESEARCH --title "Research Tasks"
```

Before filing under an initiative, read its initiative doc if present:

```bash
tickets initiatives
```

Then use the initiative's own instructions for domain-specific scope patterns,
skills, default profile, and caveats. This skill owns the generic lifecycle;
initiative docs own local routing knowledge.

## 7. Dispatch Resolution

Dispatch configuration resolves by source:

| Field | Resolution order |
|-------|------------------|
| `profile` | CLI `--profile` -> card frontmatter -> initiative `default_profile` -> `.tickets.toml` defaults |
| `skills` | CLI `--skills` -> initiative `default_skills` -> empty; inherited at create time |
| `engine`, `model`, `effort` | CLI flag -> card frontmatter -> `.tickets.toml` defaults |

Use explicit overrides for unusual work, but prefer initiative defaults for
ordinary tickets in that initiative.

Profiles often imply an engine/model through local agent-mux configuration. If
you need to override a profile-routed model, set both `engine` and `model`
explicitly on the card or CLI so dispatch can pass a coherent override.

Batch dispatch accepts comma-separated IDs:

```bash
tickets dispatch RESEARCH-001,RESEARCH-002
```

Manual dispatch serializes calls with a small stagger delay from config. For
large batches, use scheduler flow instead:

```bash
tickets dispatch-ready --dry-run
tickets dispatch-ready --max 3
```

## 8. Commands By Role

| Coordinator | Scheduler | Worker |
|-------------|-----------|--------|
| `create --initiative X --title "..." --tier worker` | `tick` | `complete <ID>` |
| `dispatch <ID>[,ID...] [--profile ...]` | `reconcile [--dry-run]` | `fail <ID> --reason "..."` |
| `dispatch-ready [--max N] [--dry-run]` | `dispatch-ready [--max N]` | `show <ID>` |
| `summary`, `board`, `list`, `show <ID>` | | |
| `cancel`, `reopen`, `block`, `close` | | |
| `init`, `edit`, `migrate`, `delete` | | |

Useful query commands:
- `tickets summary` gives compact counts by status and initiative.
- `tickets board --status dispatched` shows active work.
- `tickets board --status failed` shows failures needing attention.
- `tickets board --initiative RESEARCH` scopes the board to one initiative.
- `tickets show RESEARCH-001` displays the full card.

## 9. Reconcile, Tick, And Monitoring

`tickets tick` is the automation cycle. It runs under a file lock and performs:

1. `reconcile`
2. stall detection
3. `dispatch-ready`

Use it for scheduled or periodic processing. It is designed to be safe for a
cron job or scheduler service.

`tickets reconcile` scans `dispatched` cards and queries agent-mux status for
each `dispatch_id`. It is result-sensitive: substantial `## Result` content can
mark a ticket done even if the backend status is failure or timeout, while a
backend completion without a meaningful result can fail the ticket. Reconcile
also backfills `session_id` when available.

Run reconcile manually when a worker appears finished but the card was not
transitioned:

```bash
tickets reconcile --dry-run
tickets reconcile
```

Stall detection runs inside `tick`. It warns on long-running dispatched tickets
and can auto-fail tickets that exceed configured timeouts. Timeouts come from
tier defaults and optional per-initiative overrides.

## 10. Common Workflows

### Turn a user ask into tickets

1. Identify the initiative. If no existing initiative fits, ask instead of
   inventing one.
2. Read the initiative doc for defaults and local guidance.
3. Create the card:

```bash
tickets create --initiative RESEARCH --title "Survey vector filtering" --tier worker
```

4. Edit `## Context` and `## Scope`.
5. Stop and report created ticket IDs unless dispatch was requested.

### Dispatch requested tickets

1. Check active work:

```bash
tickets board --status dispatched
```

2. Preview readiness if using scheduler dispatch:

```bash
tickets dispatch-ready --dry-run
```

3. Dispatch within configured caps:

```bash
tickets dispatch RESEARCH-001
```

4. Monitor with `tickets summary`, `tickets board --status dispatched`, and
   `tickets reconcile --dry-run`.

### Retry a failed or blocked ticket

1. Inspect the card:

```bash
tickets show RESEARCH-001
```

2. Reopen:

```bash
tickets reopen RESEARCH-001
```

3. Edit scope/context if the previous attempt exposed missing information.
4. Dispatch again or let the scheduler pick it up.

### Aggregate after a batch

Use `awaits` when the aggregation should run after all target tickets finish,
even if some failed:

```bash
tickets create --initiative RESEARCH \
  --title "Synthesize vector filtering survey" \
  --tier deep \
  --awaits RESEARCH-001,RESEARCH-002,RESEARCH-003
```

Use `depends_on` when the aggregate requires successful upstream outputs.

## 11. Report Contract

When reporting ticket work, keep the synthesis short:

```markdown
Done:
Found:
Evidence:
Changed paths:
Blocked:
Next:
```

Evidence should include ticket IDs, relevant command output summaries, card
paths when files were created, and any dry-run or reconcile results that prove
the lifecycle state changed as intended.
