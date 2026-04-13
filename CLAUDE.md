# CLAUDE.md — agent-tickets

Filesystem-native ticket system for AI agent workflows. Go binary, markdown cards, FSM lifecycle, agent-mux dispatch.

## Build & Test

```bash
go build -o tickets ./cmd/tickets
go test ./...
```

Tests live in `cmd/tickets/main_test.go` — integration-style, exercise the full CLI through the `runCmd()` harness. Fixtures in `testdata/`. Run from repo root.

## Package Map

| Package | Responsibility |
|---------|---------------|
| `cmd/tickets/` | CLI commands. One file per command (e.g., `dispatch_cmd.go`, `complete.go`). Router in `main.go`. Shared helpers in `helpers.go`. |
| `frontmatter/` | YAML frontmatter parsing with **byte-exact round-trip**. Parses markdown into `Document` (Card struct + body sections). Writes back without mutating untouched content. |
| `fsm/` | State machine. Defines valid states (`open`, `dispatched`, `done`, `failed`, `blocked`, `closed`) and transitions. Single source of truth for lifecycle rules. |
| `dispatch/` | Dispatcher interface. `shell.go` shells out to agent-mux CLI. `mock.go` for tests. Commands never call agent-mux directly. |
| `config/` | TOML config parsing. Loads `.tickets.toml`, applies layered defaults, stall timeouts, profile engine/model maps, and guardian settings. |

## Key Invariants

1. **Byte-exact round-trip.** `frontmatter/` preserves the original file byte-for-byte except for intentional changes. Don't re-serialize YAML that wasn't modified. Tests verify this.

2. **FSM is the source of truth.** All state transitions go through `fsm.Apply()`. Commands request transitions — the FSM validates. Never manipulate `status` directly.

3. **Dispatch abstraction.** Commands use the `Dispatcher` interface, never agent-mux directly. This enables mock dispatch in tests.

4. **Config resolution order.** `profile` resolves as CLI flag > card frontmatter > initiative markdown `default_profile` > `.tickets.toml` defaults. `engine`/`model`/`effort` resolve as CLI flag > card frontmatter > `.tickets.toml` defaults. Each field tracks its `OptionSource` so dispatch logic knows provenance.

5. **Cards are the database.** No separate state store. The markdown file IS the ticket. Git history IS the audit trail.

6. **Engine flag gating.** `ShouldPassEngineFlags()` returns true only when engine comes from CLI or card level. When engine falls to config defaults and a profile is set from a higher source, engine/model/effort flags are suppressed — letting the profile define them.

7. **Terminal-state gating for `awaits`.** `IsTerminal()` returns true for `done`, `failed`, `blocked`, `closed`. `awaits` gates on terminality (any terminal state clears the dependency); `depends_on` gates on success (`done` only). Both fields can coexist; both must be satisfied before dispatch.

## How to Add a New Command

1. Create `cmd/tickets/newcmd.go` with `func runNewCmd(cfg *config.Config, args []string) error`
2. Register in `main.go` switch statement
3. Add help text in the `subcommandHelp()` function
4. Add tests in `main_test.go` using `runCmd()` harness
5. Update `coordinator-skill/SKILL.md` and `README.md`

## How to Add a New State or Transition

1. Add the state constant in `frontmatter/card.go`
2. Add transition(s) in `fsm/fsm.go` transitions map
3. Add FSM tests in `fsm/fsm_test.go`
4. Create the command file in `cmd/tickets/`
5. Update `coordinator-skill/SKILL.md`, `README.md`, and this file

## Code Conventions

- **Error messages are user-facing.** Lowercase, no periods, include ticket ID. Example: `"ticket RECON-005 is not dispatched (status: open)"`
- **Pointer fields in Card** = optional/nullable YAML fields. Non-pointer = always present.
- **Minimal dependencies.** Only yaml.v3 and toml. No frameworks, no HTTP, no database.
- **Test via CLI.** Tests exercise full command paths through `runCmd()`, not internal functions.
- **One file per command.** Command files are self-contained. Shared logic goes in `helpers.go`.

## Card Schema (frontmatter/card.go)

```go
type Card struct {
    // Identity
    ID, Initiative, Title  string
    Status                 Status    // open|dispatched|done|failed|blocked|closed
    Tier                   Tier      // worker|deep|heavy
    Tags                   []string
    Created                string
    Manual                 bool

    // Planning
    PlanRef                *string
    DependsOn              []string
    Awaits                 []string
    Skills                 []string

    // Dispatch
    DispatchID, SessionID  *string
    DispatchedAt           *string
    Profile, Engine        *string
    Model, Effort          *string

    // Lifecycle
    Attempts               int
    LastAttemptOutcome     *string
    BlockReason            *string
    DefaultProfile         *string
    DefaultSkills          []string

    // Telemetry
    Tokens                 *TokenUsage  // in, out, cache, peak_context
}
```

`reopen` clears runtime dispatch fields `dispatch_id`, `session_id`, and `dispatched_at`, then clears card-level `engine`, `model`, `effort`, and `tokens` for a fresh retry. It preserves `profile` and `last_attempt_outcome`.

## FSM States and Transitions

| From | Transitions |
|------|-------------|
| `open` | dispatch -> `dispatched`, block -> `blocked`, close -> `closed` |
| `dispatched` | complete -> `done`, fail -> `failed`, cancel -> `open` |
| `done` | reopen -> `open`, close -> `closed` |
| `failed` | reopen -> `open`, block -> `blocked`, close -> `closed` |
| `blocked` | reopen -> `open` |
| `closed` | _(terminal — no outgoing transitions)_ |

**Terminal states** (for `awaits` gating): `done`, `failed`, `blocked`, `closed`. **Non-terminal:** `open`, `dispatched`.

## Automation Notes

- `tick` runs `reconcile -> stall-detect -> dispatch-ready` under a file lock.
- Stall detection auto-fails `dispatched` tickets that exceed their timeout, prints `[STALL_WARNING]`, uses `initiatives.<NAME>.stall_timeout_minutes` when set, and falls back to the last `dispatched --` log timestamp if `dispatched_at` is missing.

## Config Notes

- `skill_path` is loaded from config to carry a custom skill path setting.
- `model_weight` and `concurrency` determine dispatch-ready engine weight caps.
- `profile_engine` and `profile_model` resolve effective engine/model for profile-routed tickets during cap accounting.
- `initiatives.<NAME>.stall_timeout_minutes` is the only initiative config consumed from `.tickets.toml`.
- Initiative `default_profile` and `default_skills` come from the initiative markdown card frontmatter, not `.tickets.toml`. Skills are inherited at create time when no explicit `--skills` flag is passed.
- Guardian mode is configured under `[guardian]`; `GuardianEnabled()` requires populated engine, model, profile, and initiative fields.

## Keeping Docs in Sync

Every logic change (new command, new state, changed behavior, new config field) must be reflected in:

- **README.md** — public reference
- **coordinator-skill/SKILL.md** — coordinator playbook
- **This file** — if it affects architecture, invariants, or conventions
