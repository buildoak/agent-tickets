---
name: ticket-work
description: >
  Coordinator playbook for authoring, dispatching, and managing tickets.
  Read this when you need to create tickets from user asks, route them to
  initiatives, dispatch workers, or monitor ticket lifecycle. Trigger: user
  asks to create/dispatch/check tickets, or you need to turn a vague ask
  into structured work.
---

# Ticket Work — Coordinator Playbook

Binary: `tickets` at `~/.local/bin/tickets`
Run all CLI commands from repo root: `/Users/otonashi/thinking/pratchett-os/`

---

## 1. System Reference

**Paths:**
- Cards: `centerpiece/tickets/cards/{INITIATIVE}/{ID}.md`
- Initiatives: `centerpiece/tickets/INITIATIVES/{NAME}.md`
- Config: `.tickets.toml` (repo root)

**Card format:** YAML frontmatter (`id`, `initiative`, `title`, `status`, `tier`, `tags`, `created`, `manual`, `plan_ref`, `depends_on`, `awaits`, `skills`, `dispatch_id`, `session_id`, `dispatched_at`, `profile`, `engine`, `model`, `effort`, `last_attempt_outcome`, `block_reason`) + four sections: `## Context`, `## Scope`, `## Result`, `## Log`.

**IDs:** `{INITIATIVE}-{seq:03d}` (e.g., RECON-009, SERENDIPITY-003). Per-initiative sequencing.

**FSM — six states:**

| State | Transitions |
|-------|-------------|
| `open` | dispatch -> `dispatched`, block -> `blocked`, close -> `closed` |
| `dispatched` | complete -> `done`, fail -> `failed`, cancel -> `open` |
| `failed` | reopen -> `open` (attempts++), block -> `blocked`, close -> `closed` |
| `blocked` | reopen -> `open` |
| `done` | reopen -> `open` (result archived to Log), close -> `closed` |
| `closed` | _(terminal — no outgoing transitions)_ |

- `closed` = conceptually dead, never retry. Use for tickets that are no longer relevant, duplicate, or impossible. Distinct from `failed` (operationally broken but retryable).
- **Terminal states:** `done`, `failed`, `blocked`, `closed`. Non-terminal: `open`, `dispatched`. `IsTerminal()` returns true for the four terminal states.
- Auto-block: after `max_retry` (default 3) consecutive failures, ticket auto-blocks.

**Dependency semantics — `depends_on` vs `awaits`:**

| Field | Gate type | Dispatches when | Use case |
|-------|-----------|-----------------|----------|
| `depends_on` | Hard — "I need their output" | All listed tickets reach `done` | Build depends on design spec |
| `awaits` | Soft — "I need them finished" | All listed tickets reach any terminal state (`done`, `failed`, `blocked`, `closed`) | Audits, reports, cleanup, aggregation |

Both fields can coexist on one ticket; both must be satisfied before dispatch. Missing `awaits` normalizes to `[]` (backward compatible).

**CLI by actor:**

| Coordinator | Scheduler | Worker |
|-------------|-----------|--------|
| `create --initiative X --title "..." --tier worker [--awaits A,B]` | `tick [--max-dispatch N]` | `complete <ID>` |
| `dispatch <ID>[,ID...] [--profile --engine --model --effort]` | `reconcile [--dry-run]` | `fail <ID> --reason "..."` |
| `dispatch-ready [--max N] [--dry-run]` | `dispatch-ready [--max N]` | `show <ID>` |
| `show <ID>`, `list`, `board`, `initiatives` | | |
| `cancel <ID>`, `reopen <ID>`, `block <ID> --reason "..."` | | |
| `close <ID> --reason "..."` | | |
| `init <NAME> --title "..."` | | |

**Tick cycle:** `tick` acquires a file lock, then runs: reconcile -> stall-detect -> dispatch-ready. Safe for LaunchAgent scheduling.

**Reconcile:** Scans all `dispatched` cards. For each, queries `agent-mux status <dispatch_id>`. It is result-sensitive: substantial `## Result` content wins over raw agent-mux exit status. `completed` without a substantial Result fails the ticket; `failed` or `timeout` with a substantial Result marks it done. Backfills `session_id` and `tokens` when available. Status query failures are tolerated up to `max_retry` before auto-failing.

---

## 2. Initiative Routing

**Mental model:** `tickets init` is called ONLY when the user explicitly asks for a new initiative. Never guess. If a ticket does not map exactly to an existing initiative, route to the best available candidate.

**Current initiatives:**

| Initiative | Scope |
|------------|-------|
| `RECON` | Unattended research, pre-initiative exploration |
| `SERENDIPITY` | Serendipity capture, enrichment, compounding |
| `OPS` | Operational/infrastructure tasks |
| `PAPER-OPS` | Paper ingestion, analysis, and synthesis (any domain) |

Each initiative doc at `centerpiece/tickets/INITIATIVES/{NAME}.md` carries dispatch defaults, operational context, and worker onboarding. Read the initiative doc before dispatching.

**Routing rules:**
- User says "look into X" with no clear project -> `RECON`
- User shares a link/article/idea for capture -> `SERENDIPITY`
- Infra, tooling, maintenance -> `OPS`
- If genuinely no initiative fits, ask the user. Do not invent one silently.
- If the user says "create an initiative for X" -> then and only then run `tickets init`.

---

## 3. Card Authoring — The Low-Effort Principle

**REQUIRED: Read the initiative card before creating any ticket.** The card lives at `centerpiece/tickets/INITIATIVES/{INITIATIVE}.md`. Initiative cards carry domain-specific guidance: scope templates, source type caveats, done criteria, dispatch mechanics, and `default_profile` (the profile may already include all the domain knowledge the worker needs — do not duplicate it with skills). This skill owns the general ticket lifecycle; initiative cards own the per-domain knowledge. If no initiative card exists, proceed with the general patterns below.

**Mental model:** Tickets should be low-effort to create. Give the worker a good enough scope — do not do the job for them. The coordinator writes two things:

1. **A succinct ask** — what we want, plain language.
2. **A verification gate** — semantic definition of "done" (what the result looks like when it is right, not a step-by-step recipe).

### Good vs bad Scope

**Good** — concise ask + clear gate:
```markdown
## Scope
Survey compression techniques for 3DGS: quantization, pruning,
codebook approaches, compact representations. For each: method name,
paper/repo, compression ratio, quality impact (PSNR/SSIM delta).
Note production-ready vs research-only.

**Done means:** Result has a structured survey of 4+ techniques
with compression ratios and quality tradeoffs.
```

**Bad** — step-by-step instructions that do the worker's thinking:
```markdown
## Scope
1. Open Google Scholar and search "3DGS compression"
2. Find the LightGaussian paper and summarize it
3. Find the HAC++ paper and summarize it
4. Create a markdown table with columns: Method, Paper URL, ...
5. Write a conclusion paragraph comparing them
```

The first tells the worker WHAT to deliver and HOW to know it is done. The second micromanages HOW to do the work — that is the worker's job.

### Context section

`## Context` is cold-start onboarding. Include only what a zero-context worker needs:
- Files to read (skill paths, related tickets, project docs)
- Brief background if the domain is non-obvious
- Working directory when it differs from repo root

Keep it minimal. Over-stuffing Context wastes the worker's context window.

### Skills vs Profiles

**Profiles carry domain knowledge.** When an initiative sets a `default_profile` (e.g., `paper-ops-worker`), that profile's system prompt already includes the specialized instructions the worker needs. Do not add a skill that duplicates what the profile provides — the profile IS the skill for the worker.

**Skills add tooling, not knowledge.** Add skills only when the worker needs capabilities the profile doesn't provide:
- `web-search` — worker needs to fetch URLs or search the web
- `pratchett-read` — worker needs to search the knowledge base

Skills are set in card frontmatter: `skills: [web-search]`. Initiatives can set `default_skills` in their frontmatter (e.g., `default_skills: [web-search]`), which are inherited by new tickets at creation time when no explicit `--skills` flag is passed.

### Context files

Reference files the worker must read in `## Context`. Do not paste their contents — give paths. The worker reads them.

### Tier selection

| Tier | When to use |
|------|-------------|
| `worker` | Standard tasks: research, surveys, writing, audits. Default. |
| `deep` | Multi-step tasks requiring extended reasoning. |
| `heavy` | Complex tasks needing stronger models or longer timeouts. |

**Not a ticket:** Trivial tasks (< 2 min of work) — just do them inline or dispatch a subagent directly. Not everything needs a card.

**Not a ticket:** Tasks requiring real-time human dialogue or iterative refinement — those are conversations, not fire-and-forget units.

---

## 4. Dispatch Configuration

### Profile routing

Profiles do not come from ticket tier. `dispatch` resolves `profile` through the standard chain, regardless of whether the card is `worker`, `deep`, or `heavy`:

- CLI `--profile`
- card frontmatter `profile`
- initiative markdown `default_profile`
- `.tickets.toml` `[defaults].profile`

Use an explicit profile override when the task needs one; tier alone does not switch profiles.

### Override heuristics

Resolution order:
- `profile`: CLI `--profile` -> card frontmatter -> initiative markdown `default_profile` -> `.tickets.toml` global default
- `skills`: CLI `--skills` -> initiative markdown `default_skills` -> empty (inherited at create time)
- `engine` / `model` / `effort`: CLI flag -> card frontmatter -> `.tickets.toml` global defaults

Per-initiative engine/model/profile preferences live in the initiative doc (`centerpiece/tickets/INITIATIVES/{NAME}.md`), not here. This skill owns the generic dispatch process; initiative docs own the per-initiative config.

Override when:
- Task needs Claude instead of Codex: `--engine claude --model claude-sonnet-4-6 --effort high`
- Task needs stronger model on Codex: `--profile ticket-worker-heavy`
- Research task needs web: set `skills: [web-search]` in card frontmatter (does not affect engine choice)

### Model override rule (critical)

When a profile handles engine selection (e.g., `paper-ops-worker` → gemini), the dispatch code omits `--engine` and `--model` from the agent-mux call, letting the profile define them. This means **setting only `model:` on a card without `engine:` has no effect** — the model is suppressed alongside the engine, and the profile's default model is used.

**To override the model while keeping a profile-routed engine:** set BOTH `engine:` and `model:` on the card frontmatter. Both must resolve as `SourceCard` for the dispatch code to pass them through to agent-mux.

```yaml
# Wrong — model is silently ignored, profile's default model wins:
engine: null
model: gemini-3.1-pro-preview

# Correct — both passed to agent-mux:
engine: gemini
model: gemini-3.1-pro-preview
```

The logic: `ShouldPassEngineFlags` returns true only when engine comes from CLI or card level. When engine falls to config defaults and a profile is set from a higher source (initiative/card), engine flags are suppressed — and model/effort are gated on the same flag.

### Config defaults (`.tickets.toml`)

```toml
engine  = "codex"
model   = "gpt-5.4-mini"
effort  = "xhigh"
profile = "jenkins-junior"
max_retry = 3
stagger_seconds = 15
```

### Concurrency limits

**Respect engine concurrency caps from `.tickets.toml`.** Before dispatching, check how many tickets are already `dispatched` per engine. Current limits:

| Engine | Max concurrent |
|--------|---------------|
| codex | 5 |
| gemini | 4 |
| claude | 3 |

`dispatch-ready` enforces these caps automatically. Manual `tickets dispatch` does NOT — the coordinator must check `tickets board --status dispatched` and count before dispatching a batch. Exceeding limits risks agent-mux failures or queuing delays.

### Batch dispatch

Comma-separated IDs: `tickets dispatch RECON-010,RECON-011,RECON-012`

Stagger: 15s pause between each dispatch in a batch (configurable via `stagger_seconds`). Prevents overloading the engine.

### Dry run

Always available for validation:
- `tickets dispatch-ready --dry-run` — shows what would be dispatched
- `tickets reconcile --dry-run` — shows what state transitions would happen

Use `--dry-run` before real dispatch when batch size > 3 or when unsure about readiness.

---

## 5. Monitoring & Lifecycle

### Agent-efficient commands (use these first)

`tickets summary` — counts by status × initiative, ~100 tokens. Start here.
`tickets board --status dispatched` — only active work, resolved engine/model names.
`tickets board --status failed` — only failures needing attention.
`tickets board --initiative PAPER-OPS` — scoped to one initiative.

### Full views (human-facing, ~2k+ tokens)

`tickets board` — full kanban view, all tickets. Use `--status` or `--initiative` to filter. Tickets with unresolved soft dependencies show an `(awaits)` suffix to distinguish them from hard-dep blocks.
`tickets list --status dispatched` — all currently running tickets.
`tickets show <ID>` — full card with dispatch fields and log.

### Reconcile

Runs automatically inside `tick`. Manual run: `tickets reconcile`.

What it does: polls `agent-mux status` for each dispatched ticket, then decides based on both backend status and `## Result` content. A substantial Result can cause reconcile to mark a ticket `done` even when agent-mux reports `failed` or `timeout`; a missing/placeholder Result can cause a `completed` run to be marked `failed`. It also backfills `session_id` and `tokens` on done/failed cards that are missing them.

Run manually when: you suspect a worker finished but the card was not updated (worker crashed after writing Result but before calling `tickets complete`).

### Stall detection

Runs automatically inside `tick` after reconcile.

What it does:
- Finds `dispatched` tickets whose elapsed time exceeds their stall timeout.
- Prints `[STALL_WARNING]` lines for visibility.
- Auto-fails stalled tickets using the same failure path as reconcile.
- Uses `initiatives.<NAME>.stall_timeout_minutes` when configured, otherwise tier defaults from `[stall_timeout_minutes]`.
- Falls back to the last `dispatched --` log entry when `dispatched_at` is absent or invalid.

### Reopen/retry

`tickets reopen <ID>` — moves failed/blocked/done back to open. On failed: increments `attempts`, clears dispatch fields. On done: archives existing Result to Log before clearing.

Retry flow: reopen -> (optionally edit Scope/Context) -> dispatch again.

### Terminal states

| Action | From | When to use |
|--------|------|-------------|
| `cancel <ID>` | dispatched | Worker is running but the task is no longer needed. Returns to open, no attempt increment. |
| `fail <ID> --reason "..."` | dispatched | Worker cannot complete. Called by worker or reconcile. |
| `block <ID> --reason "..."` | open, failed | Task is stuck on an external dependency or needs human input. Requires a reason. |
| `close <ID> --reason "..."` | open, done, failed | Conceptually dead. Never retry. Requires a reason. |

### Workflow: user ask to ticket

**Default flow: create open tickets, let the scheduler dispatch them.**

The tick scheduler (`tickets tick` via LaunchAgent) runs `dispatch-ready` which respects concurrency caps, stagger delays, and `max_dispatch_per_tick`. It is the safe, rate-limited path. The coordinator should NOT manually dispatch unless the user explicitly asks.

1. Parse user's ask. Identify initiative (Section 2).
2. Read the initiative card at `centerpiece/tickets/INITIATIVES/{INITIATIVE}.md` for domain-specific ticket guidance (scope templates, caveats, done criteria).
3. `tickets create --initiative X --title "..." --tier worker`
4. Edit the created card: write `## Context` and `## Scope` using initiative card guidance + Section 3 principles.
5. **Stop here.** Report created ticket IDs. The scheduler picks them up.

**When the user explicitly asks to dispatch manually:**

6. **First: check in-flight count.** Run `tickets board --status dispatched` and count how many are running per engine. Compare against concurrency caps in `.tickets.toml`. If at or near the cap — tell the user and wait, or dispatch only enough to fill remaining slots.
7. `tickets dispatch <ID>[,ID...] --engine X --model Y --effort Z` (with explicit overrides per Section 4). Never dispatch more than the engine's concurrency cap in one batch.
8. Monitor via `tickets board` or `tickets list --status dispatched`.
9. Reconcile handles completion. Review result on the card.

**Why this matters:** Codex kills processes with SIGTERM when too many instances launch simultaneously. Bulk manual dispatches (7+ at once) cause instant `killed_by_user` failures with 0 tokens consumed. The scheduler's stagger and caps prevent this.

---

## 6. GUARDIAN Audit Initiative

GUARDIAN is an audit initiative that uses standard ticket mechanics — no special infrastructure. GUARDIAN tickets use `awaits` (not `depends_on`) to point at batches of ~10 target tickets. This means a GUARDIAN ticket dispatches once all its targets reach any terminal state, regardless of whether they succeeded or failed.

**How it works:**
1. A GUARDIAN ticket is filed `open` with `awaits: [PAPER-OPS-101, PAPER-OPS-102, ...]` listing ~10 target ticket IDs.
2. The scheduler's `dispatch-ready` checks the `awaits` gate: all listed tickets must be terminal (`done`, `failed`, `blocked`, or `closed`).
3. Once the gate clears, the GUARDIAN ticket dispatches automatically like any other ticket.
4. The guardian-worker runs a pre-flight status check on each target as defense-in-depth: `done` tickets get audited, `failed`/`blocked`/`closed` are SKIPPED, `open`/`dispatched` are PENDING (should not happen if awaits gating worked correctly).
5. GUARDIAN tickets are configured in `[guardian]` in `.tickets.toml` (engine, model, profile, initiative).

**Coordinator role:** Create the GUARDIAN ticket with `--awaits` listing the target batch, file it `open`, and let the scheduler handle the rest. No manual dispatch needed.
