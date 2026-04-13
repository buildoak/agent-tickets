# agent-tickets v5 Deep Audit (gemini)

## Executive Summary
The agent-tickets tool shows a high degree of fidelity to its specification in the core data model, state machine, and configuration handling. The `frontmatter` package, while complex, is well-tested for round-trip fidelity, which is critical. The FSM logic is a clean and direct implementation of the spec's transition table. The biggest risk is the unverifiable CLI verb implementation; since the `cmd/` directory contents were inaccessible during the audit, spec compliance for the 17 CLI verbs and their specific behaviors (like auto-blocking or dependency checks) remains unconfirmed. The project structure also deviates from the spec's proposed layout.

## Verb-by-Verb Drill
**VERDICT: UNVERIFIABLE**

The implementation of the 17 CLI verbs, expected to be in the `cmd/` directory, was not accessible for this audit. As such, a line-by-line comparison of the verb implementations against the spec is not possible. This is a critical gap in the audit.

Based on the spec alone (Section 10), the verbs are well-defined. However, key logic is specified to be in these handlers:
- **`create`**: Correctly assigning sequence numbers.
- **`dispatch`**: Checking for `done` status in dependencies.
- **`fail`**: Implementing the `attempts >= max_retry` auto-block logic. This is **NOT** in the core FSM package.
- **`delete`**: Checking for dependents and enforcing `--cascade`.
- **`complete`**: Hard gate for non-trivial `## Result` section.

Without seeing the code, compliance for these crucial features is unknown.

## Data Model
**VERDICT: PASS**

The Go structs in `frontmatter/card.go` and `frontmatter/document.go` are a faithful implementation of the data model described in Spec Section 3.

| Field | Spec Type | Go Type | `yaml` Tag | Correct? | Notes |
|---|---|---|---|---|---|
| `id` | `string` | `string` | `id` | Yes | |
| `initiative` | `string` | `string` | `initiative` | Yes | |
| `title` | `string` | `string` | `title` | Yes | |
| `status` | `Status` | `Status` | `status` | Yes | |
| `tier` | `Tier` | `Tier` | `tier` | Yes | |
| `tags` | `[]string` | `[]string` | `tags` | Yes | |
| `created` | `string` | `string` | `created` | Yes | |
| `manual` | `bool` | `bool` | `manual` | Yes | |
| `plan_ref` | `*string` | `*string` | `plan_ref` | Yes | Correctly a pointer for optionality. |
| `depends_on` | `[]string` | `[]string` | `depends_on` | Yes | |
| `dispatch_id` | `*string` | `*string` | `dispatch_id` | Yes | Correctly a pointer. |
| `session_id` | `*string` | `*string` | `session_id` | Yes | Correctly a pointer. |
| `engine` | `*string` | `*string` | `engine` | Yes | Correctly a pointer. |
| `model` | `*string` | `*string` | `model` | Yes | Correctly a pointer. |
| `effort` | `*string` | `*string` | `effort` | Yes | Correctly a pointer. |
| `attempts` | `int` | `int` | `attempts` | Yes | |
| `last_attempt_outcome` | `*string` | `*string` | `last_attempt_outcome` | Yes | Correctly a pointer. |
| `block_reason` | `*string` | `*string` | `block_reason` | Yes | Correctly a pointer. |
| `tokens` | `*TokenUsage` | `*TokenUsage` | `tokens` | Yes | Correctly a pointer. |

The `frontmatter` parsing and writing logic correctly preserves the body bytes on round-trip, which is essential.

## FSM
**VERDICT: PARTIAL**

The `fsm/fsm.go` package correctly implements the state transition graph. However, a key piece of logic is missing from this package and its implementation is unverifiable.

| Transition | Spec | Code | Correct? |
|---|---|---|---|
| `open` → `dispatched` | Yes | Yes | Yes |
| `open` → `blocked` | Yes | Yes | Yes |
| `dispatched` → `done` | Yes | Yes | Yes |
| `dispatched` → `failed` | Yes | Yes | Yes |
| `dispatched` → `open` | Yes | Yes | Yes |
| `failed` → `open` | Yes | Yes | Yes |
| `failed` → `blocked` | Yes | Yes | Yes |
| `blocked` → `open` | Yes | Yes | Yes |
| `done` → `open` | Yes | Yes | Yes |

**Side Effects:**
- **Reopen from `failed`:** Correctly increments `attempts`. **PASS**
- **Reopen from `done`:** Correctly archives result and does not increment `attempts`. **PASS**
- **Cancel:** Correctly sets `last_attempt_outcome` to "cancelled" and does not increment `attempts`. **PASS**
- **Auto-block on `fail`:** The logic for checking `attempts >= max_retry` is not in the `fsm` package. The spec says this happens on the transition to `failed`. This logic must be in the `tickets fail` command handler, which is not visible. **GAP**

## Config & Dispatch
**VERDICT: PASS**

The configuration loading and dispatch execution logic are compliant with the spec.

- **Config Precedence:** `config/config.go` correctly implements the `env > .tickets.toml > default` precedence rule.
- **Config Schema:** The `Config` struct matches the fields in the spec's `.tickets.toml` example.
- **Dispatch Contract:** `dispatch/shell.go` correctly constructs the `agent-mux` command-line arguments for both `dispatch` and `status` calls, including optional flags. It correctly parses the JSON responses.
- **Spec Inconsistency:** The spec's "Agent-Mux CLI Contract" (Section 7) omits the `--preamble` flag, but other sections (6, 15) require it. The code correctly implements the preamble, siding with the more detailed requirement. This is a minor spec issue, not a code bug.

## Bugs Found
1.  **[Minor] Inadequate Error Reporting in Dispatcher:** In `dispatch/shell.go`, if the `agent-mux` command fails, the error returned (`runJSON`) does not include the `stderr` from the failed process. This makes debugging user-facing errors (e.g., `agent-mux` not found, invalid profile) unnecessarily difficult. The `*exec.ExitError` contains the `Stderr` field, which should be included in the returned error message.
2.  **[Spec-Code Mismatch] Project Structure Deviation:** The spec (Section 23) mandates a repo structure using `internal/card/`, `internal/state/`, etc. The actual codebase uses top-level packages like `frontmatter/` and `fsm/`. This is a significant deviation from the specified implementation plan and makes the code public when it was intended to be internal.

## Code Quality Issues
1.  **[Minor] Incomplete Tests:** `dispatch/dispatch_test.go` has a test `TestShellDispatcherDispatchBuildsArgs` that does not cover all argument variations. It tests a single combination of flags and misses testing the `--preamble` flag and cases where optional flags are omitted.
2.  **[Trivial] No `go vet` issues found.** The visible codebase is clean according to `go vet`.

## Verdict
**Verdict: NOT SPEC-COMPLETE.**

The `agent-tickets` tool cannot be considered spec-complete or production-ready.

While the foundational packages (`frontmatter`, `fsm`, `config`, `dispatch`) are well-implemented and largely adhere to the spec, the complete inability to audit the `cmd/` directory is a critical failure. The CLI verbs contain essential business logic (sequencing, dependency checks, auto-blocking, completion gates) that is currently a black box. The audit identified a significant deviation in project structure from the spec and a minor bug in error handling.

**Must-Fix Before Production Use:**
1.  **Make `cmd/` source code available** for a full verb-by-verb audit. This is the highest priority.
2.  **Correct the project structure** to align with the spec's `internal/` package layout (`internal/card`, `internal/state`, etc.) or update the spec to reflect the as-built structure.
3.  **Improve error reporting** in the dispatcher to include `stderr` for better diagnostics.
