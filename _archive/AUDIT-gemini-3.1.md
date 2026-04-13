# agent-tickets v5 Deep Audit (Gemini 3.1 Pro Preview)

## Executive Summary
The `agent-tickets` Go CLI implements the v5 Ticket Engine spec with exceptionally high fidelity, leveraging a custom, byte-preserving frontmatter parser, a strict FSM, and a clean domain model. The vast majority of the 17 specified verbs behave exactly as defined, correctly enforcing constraints, lifecycle hooks, and dependency validation. However, a critical failure in enforcing the `complete` gate was found—placeholder text in `## Result` is not rejected despite a helper function existing for that exact purpose. Additionally, a rogue undocumented `edit` command exists, and there are structural risks around edge-case circular dependency checks. Overall, it's robust but requires a quick patch for the completion gate to be 100% spec-compliant.

## Verb-by-Verb Drill
- **init**: PASS. Correctly scaffolds initiative directory and metadata. Prevents overwriting.
- **create**: PASS. Resolves `--depends-on` correctly, writes correctly scaffolded markdown file.
- **dispatch**: PASS. Applies config precedence, checks scopes, resolves dependencies, and injects previous attempt context into the preamble.
- **complete**: **FAIL**. The spec mandates refusing completion if the `## Result` section contains only placeholder text (e.g., "[filled by..."). A helper function `isPlaceholder` exists in `helpers.go` but is NEVER called in `cmdComplete`. It only checks for empty strings. Furthermore, the error message does not match the spec's exact required string.
- **fail**: PASS. Handles transitions correctly and auto-blocks after `maxRetry` attempts.
- **cancel**: PASS. Preserves partial writes without incrementing attempts.
- **reopen**: PASS. Distinguishes between reopening from `failed` (increments attempts) versus `done`/`blocked` (does not increment attempts). Correctly archives results for `done` tickets.
- **block**: PASS. Correctly accepts `open` or `failed` tickets and requires a reason.
- **delete**: PASS. Enforces dependency graph constraints and prevents deletion of dispatched tickets. Cascades correctly.
- **show**: PASS. Outputs raw frontmatter or JSON with annotations.
- **list**: PASS. Filters correctly and outputs JSON with annotations.
- **board**: PASS. Annotations map correctly (e.g., `waiting`, `queued`, `running`).
- **initiatives**: PASS. Correctly aggregates ticket status counts.
- **migrate**: PASS. Safely rewrites `depends_on` in other tickets and rejects if dependency rewrite scope exceeds 100 tickets.
- **reconcile**: **PARTIAL**. Correctly queries `agent-mux` and handles stale states, but shares the same bug as `complete` where it blindly accepts placeholder text as a valid `## Result`.
- **dispatch-ready**: PASS. Sorts eligible tickets by creation date and ignores manual tickets.
- **tick**: PASS. Implements file locking to prevent concurrent runs. Correctly runs reconcile and dispatch-ready.
- ***edit***: **EXTRA**. An undocumented `tickets edit TICKET-ID` verb exists in the codebase that opens `$EDITOR` and validates dependencies on save. Not part of the 17 verbs listed in the spec.

## Data Model
- The Go struct `Card` matches the YAML definition from Section 3 of the spec field-for-field.
- `tokens` is accurately modeled as a nested `TokenUsage` struct and handled correctly.
- Pointers (`*string`) are appropriately used for `dispatch_id`, `session_id`, `engine`, `model`, `effort`, `last_attempt_outcome`, `block_reason`, and `plan_ref` to ensure they marshal to `null` in YAML.
- `frontmatter` parsing perfectly preserves the document body byte-for-byte and retains the original line endings, honoring the "Files are the only truth" mandate. Fields unchanged in code remain byte-exact in the file.

## FSM
- The transition table in `fsm/fsm.go` aligns precisely with the spec's state diagram.
- `open` -> `dispatched`, `open` -> `blocked` are correctly enforced.
- `dispatched` -> `open` (`cancel`) preserves dispatch fields but increments no attempts.
- `done` -> `open` (`reopen`) sets `archiveResult: true` and clears dispatch fields without incrementing attempts.
- `failed` -> `open` (`reopen`) sets `incrementAttempts: true` and clears dispatch fields.
- Auto-block (`maxRetry` reached) is seamlessly handled by forcing an FSM `TransitionBlock` inside `cmdFail`.

## Config & Dispatch
- Config resolution precedence (Environment -> `.tickets.toml` -> Defaults) is implemented correctly in `config.Load()`.
- The dispatch interface securely maps to `agent-mux dispatch` and `agent-mux status --json` with exact flag passing (`--profile`, `--prompt-file`, `--engine`, `--model`, `--effort`).

## Bugs Found
1. **Critical: Placeholder Bypass in Completion Gate**
   - *File*: `cmd/tickets/complete.go:39` & `cmd/tickets/reconcile.go:119`
   - *Issue*: `isPlaceholder()` exists in `helpers.go` but is dead code. The check only does `resultText == ""`. Agents leaving `[Filled by the executing agent...` will incorrectly pass the gate and be marked `done`.
2. **Missing Spec Error Message**
   - *File*: `cmd/tickets/complete.go:40`
   - *Issue*: Returns a generic `cannot complete ticket with empty ## Result` instead of the spec-mandated multi-line error instructing the agent to write its result.
3. **Logic Flaw: Infinite Recursion Risk in Dependency Check**
   - *File*: `cmd/tickets/helpers.go:379` (`maxDependencyDepth`)
   - *Issue*: `maxDependencyDepth` recursively traverses the graph without a `visited` set. While `detectCycle` runs first to ensure the graph *starting from the current node* is acyclic, if a user manually edits a markdown file and introduces a disjoint cycle elsewhere, `maxDependencyDepth` might traverse into it during validation, leading to a stack overflow panic.
4. **Undocumented CLI Verb**
   - *File*: `cmd/tickets/edit.go`
   - *Issue*: Exposes `tickets edit`, which is absent from Section 10 of the spec.

## Code Quality Issues
- **Dead Code**: `isPlaceholder` is completely unused.
- **Lost Update Risk (Minor)**: While `WriteFile` uses atomic renames, there is no file locking to prevent a race condition if `reconcile` and an agent invoking `complete` happen in the exact same millisecond. Given the operational scale, this is an acceptable risk but worth noting.
- **Test Coverage Gap**: The tests in `main_test.go` fail to test the placeholder refusal in `TestCompleteRequiresResult`, which allowed the core bug to slip through.

## Verdict
The system is largely spec-complete and structurally sound. The frontmatter handling and FSM logic are exemplary. However, **it fails the primary hard gate** defined in the spec: refusing placeholder text on completion. This must be fixed in both `cmdComplete` and `reconcileDispatchedTicket` before the system can be trusted to handle automated agent outputs. The `maxDependencyDepth` recursion risk must also be patched to prevent panics, and the `edit` command should either be documented or removed.