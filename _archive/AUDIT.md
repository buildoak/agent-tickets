# agent-tickets v5 Spec Audit

## Summary
This repository is an early partial implementation of the v5 design, not a spec-complete CLI. The strongest areas are the standalone frontmatter parser/writer, a pure FSM package, and a small command set that covers part of the basic lifecycle. The biggest gaps are the missing verb surface (`board`, `initiatives`, `migrate`, `reconcile`, `dispatch-ready`, `tick`), the lack of any `.tickets.toml` config layer, and a dispatch path that does not actually use the Section 7 `agent-mux` contract. The biggest risks are behavioral drift in lifecycle metadata and dispatch traceability: ticket `created` timestamps do not match the spec format, `session_id` is never populated, dispatch is stubbed with synthetic IDs, dependency cycle/depth checks are absent, and JSON read output does not include computed board annotations.

## A. Verb Coverage

| verb | spec status | impl status | gap notes |
| --- | --- | --- | --- |
| `create` | in spec | partial | Implemented in `cmd/tickets/create.go`, `cmdCreate` (`--initiative`, `--title`, `--tier`, `--manual`, `--depends-on`). Missing cycle detection/max depth validation for `depends_on`; writes `created` as RFC3339 timestamp instead of date-only; no dependency existence validation at create time. |
| `dispatch` | in spec | partial | Implemented in `cmd/tickets/dispatch_cmd.go`, `cmdDispatch`. Only accepts one ID, not comma-separated batch dispatch. Does not resolve defaults from `.tickets.toml`; does not require engine/model when defaults are absent; does not call `agent-mux`; fabricates `dispatch_id`; never sets `session_id`; does not inject retry preamble; does not use the `dispatch` package. |
| `complete` | in spec | partial | Implemented in `cmd/tickets/complete.go`, `cmdComplete`. Enforces non-empty `## Result`, but only whitespace-empty, not placeholder-text empty. Does not query token usage and does not populate `tokens`. |
| `fail` | in spec | partial | Implemented in `cmd/tickets/fail.go`, `cmdFail`. Manual `--reason` path exists and status transition is enforced. Auto-block threshold matches the spec’s described 4th-dispatch behavior, but `max_retry` is hardcoded to `3` instead of config-driven. No reconcile-driven failure path. |
| `cancel` | in spec | partial | Implemented in `cmd/tickets/cancel.go`, `cmdCancel`. Preserves dispatch fields and partial result content, which matches spec intent. No extra issues beyond the missing real dispatch metadata. |
| `reopen` | in spec | partial | Implemented in `cmd/tickets/reopen.go`, `cmdReopen`. Correctly increments `attempts` only from `failed`, clears dispatch fields on failed/blocked/done, clears `block_reason` on blocked, and archives previous `Result` on done. Archive format is ad hoc; there is no explicit structured result archival format from the spec. |
| `block` | in spec | implemented | Implemented in `cmd/tickets/block.go`, `cmdBlock`. Supports `open -> blocked` and `failed -> blocked` with a required reason. |
| `delete` | in spec | partial | Implemented in `cmd/tickets/delete_cmd.go`, `cmdDelete`. Refuses `dispatched` and supports `--cascade`, but error output does not show the dependency branch or confirmation guidance described in the spec. |
| `show` | in spec | partial | Implemented in `cmd/tickets/show.go`, `cmdShow`. `--json` returns only frontmatter, not frontmatter plus computed annotations. |
| `list` | in spec | partial | Implemented in `cmd/tickets/list.go`, `cmdList`. Supports `--initiative`, `--status`, `--tag`, `--json`, but JSON output is only `[]frontmatter.Card`; no computed annotations. |
| `board` | in spec | missing | No command wired in `cmd/tickets/main.go`. |
| `initiatives` | in spec | missing | No command wired in `cmd/tickets/main.go`. |
| `init` | in spec | partial | Implemented in `cmd/tickets/init.go`, `cmdInit`. Correctly errors if the initiative directory already exists and scaffolds the three required sections. `created` is written as RFC3339 instead of date-only `YYYY-MM-DD`. |
| `migrate` | in spec | missing | No command. No resequencing or `depends_on` rewrite support. |
| `reconcile` | in spec | missing | No command. No orphan-dispatch recovery or token backfill. |
| `dispatch-ready` | in spec | missing | No command. No FIFO scheduler dispatch. |
| `tick` | in spec | missing | No command. No `.tick.lock` locking. |
| `edit` | not in spec | extra | Implemented in `cmd/tickets/edit.go`, `cmdEdit`. Useful locally, but it is an undocumented extra verb. |

## B. Frontmatter Schema

| field | in spec? | in code? | tested? | notes |
| --- | --- | --- | --- | --- |
| `id` | yes | yes | yes | `frontmatter.Card.ID`. Covered by fixtures and CLI tests. |
| `initiative` | yes | yes | yes | `frontmatter.Card.Initiative`. |
| `title` | yes | yes | yes | `frontmatter.Card.Title`. Multiline YAML parse tested. |
| `status` | yes | yes | yes | `frontmatter.Card.Status`. |
| `tier` | yes | yes | yes | `frontmatter.Card.Tier`. |
| `tags` | yes | yes | yes | Empty-slice serialization tested. |
| `created` | yes | yes | partial | Field exists, but CLI writes RFC3339 via `timestamp()` and `time.Now().Format(time.RFC3339)` instead of date-only. |
| `manual` | yes | yes | partial | Present and created by CLI; no dedicated manual-behavior tests beyond storage. |
| `plan_ref` | yes | yes | yes | Pointer field round-trip tested. Not settable via CLI, which matches spec. |
| `depends_on` | yes | yes | partial | Serialization covered, but cycle detection/max depth validation are absent. |
| `dispatch_id` | yes | yes | yes | Present and round-tripped, but CLI dispatch writes a synthetic value instead of `agent-mux` output. |
| `session_id` | yes | yes | yes | Present and round-tripped, but no CLI path ever sets it. |
| `engine` | yes | yes | yes | Present and round-tripped. |
| `model` | yes | yes | yes | Present and round-tripped. |
| `effort` | yes | yes | yes | Present and round-tripped. |
| `attempts` | yes | yes | partial | Present, tested through fail/reopen lifecycle, but `max_retry` is hardcoded and there is no config. |
| `last_attempt_outcome` | yes | yes | yes | Present and partially behavior-tested through fail/cancel FSM side effects. |
| `block_reason` | yes | yes | yes | Present and behavior-tested through block/reopen. |
| `tokens` | yes | yes | partial | Shape matches spec. Round-trip tested from fixture, but no CLI path populates it on complete or reconcile. |
| `updated` | no | no | no | Mentioned in the audit checklist, but not present in Section 3 of the v5 spec. |
| `dispatched_at` | no | no | no | Not present in Section 3 of the v5 spec. |
| `completed_at` | no | no | no | Not present in Section 3 of the v5 spec. |
| `failed_at` | no | no | no | Not present in Section 3 of the v5 spec. |
| `max_attempts` | no | no | no | The spec uses global `max_retry` config, not a per-card `max_attempts` field. |

Assessment:

- The declared Go schema in `frontmatter/card.go` matches the actual Section 3 ticket fields closely.
- The implementation does not match the spec’s type/format requirement for `created`: tickets and initiatives are written with RFC3339 timestamps, not `2006-01-02`.
- Body preservation is tested and appears to work for parse/serialize and single-field header mutation. What is not tested is byte-for-byte whole-file fidelity of the YAML header after a no-op round trip; the writer re-marshals YAML (`frontmatter/write.go`), so header formatting, quoting, ordering, and comments are not preserved byte-for-byte.

## C. FSM Transitions

| from | to | spec? | impl? | tested? | notes |
| --- | --- | --- | --- | --- | --- |
| `open` | `dispatched` | yes | yes | yes | `fsm.Apply(..., TransitionDispatch)` and `cmdDispatch`. |
| `open` | `blocked` | yes | yes | yes | `fsm` and `cmdBlock`. |
| `dispatched` | `done` | yes | yes | yes | `fsm` and `cmdComplete`. |
| `dispatched` | `failed` | yes | yes | yes | `fsm` and `cmdFail`. |
| `dispatched` | `open` via `cancel` | yes | yes | yes | `fsm` and `cmdCancel`; preserves dispatch fields. |
| `failed` | `open` via `reopen` | yes | yes | yes | `fsm` and `cmdReopen`; increments `attempts`. |
| `failed` | `blocked` | yes | yes | yes | `fsm`, `cmdBlock`, and auto-block logic in `cmdFail`. |
| `blocked` | `open` via `reopen` | yes | yes | yes | `fsm` and `cmdReopen`; clears `block_reason`. |
| `done` | `open` via `reopen` | yes | yes | yes | `fsm` and `cmdReopen`; archives prior result and clears `Result`. |

Invalid transitions:

- The pure FSM rejects unsupported transitions and has dedicated invalid-transition tests in `fsm/fsm_test.go`.
- The command layer also guards state in most verbs, but coverage is incomplete: there are no command tests for invalid `show`, `list`, `dispatch`, `fail`, `cancel`, `block`, `reopen`, or `delete` paths beyond a few happy cases.

Notable gaps:

- The FSM package itself is structurally aligned with the spec, but the broader lifecycle semantics are incomplete because `complete` does not record tokens, `fail` uses hardcoded `3`, and there is no reconcile path.

## D. Dispatch Contract

The dispatch adapter package models the Section 7 contract reasonably well on paper:

- `dispatch.DispatchOptions` includes `Profile`, `Engine`, `Model`, `Effort`, `TicketPath`, and `Preamble`.
- `dispatch.ShellDispatcher.dispatchArgs()` builds `agent-mux dispatch --profile PROFILE --prompt-file FILE [--engine E --model M --effort X]`.
- `dispatch.ShellDispatcher.statusArgs()` builds `agent-mux status DISPATCH_ID --json`.
- `DispatchResult` and `StatusResult` match the expected JSON shapes for `dispatch_id`, `session_id`, `status`, and token usage.

But the actual CLI does not use that adapter at all:

- `cmd/tickets/dispatch_cmd.go` never imports `dispatch`.
- It does not call `agent-mux dispatch`.
- It synthesizes `dispatch_id` as `pending-<timestamp>`.
- It never records `session_id`.
- It does not resolve or pass `profile`.
- It does not implement retry preamble injection despite having a `Preamble` field in `DispatchOptions`.
- It does not support batch dispatch.
- It does not query status during `complete` because `complete` never uses `agent-mux status`.

Net: the `dispatch` package is closer to the spec than the CLI command is, but today the real dispatch path is effectively a stub/shim and does not satisfy Section 7.

## E. Config Schema

Spec status:

- The v5 spec defines env/config precedence for `base_dir`, `agent_mux_bin`, and `max_retry`, plus `[defaults]` for `engine`, `model`, `effort`, and `profile`.
- The audit checklist also asks about `max_concurrent`; that key does not appear in the v5 spec text. The scheduler surface uses `dispatch-ready --max N` and `tick --max-dispatch N` instead.

Implementation status:

- No `.tickets.toml` parser exists.
- No config package exists.
- `resolveBaseDir()` supports `--base` and `TICKETS_BASE_DIR` only.
- There is no `TICKETS_AGENT_MUX_BIN` handling in the CLI path.
- `max_retry` is hardcoded as `3` in `cmd/tickets/fail.go`.
- Dispatch defaults for `engine`, `model`, `effort`, and `profile` do not exist.

Net: config support is materially missing.

## F. Test Quality

Per-package assessment:

- `frontmatter`: strongest package. Good fixture coverage across states, pointer/null behavior, token map shape, section extraction/mutation, file read/write, and some parse edge cases. The main missing test is true whole-document byte-for-byte round-trip fidelity, especially header formatting preservation and comment preservation.
- `fsm`: good direct coverage of all valid transitions and a representative set of invalid ones. Missing only deeper end-to-end command assertions for transition side effects.
- `dispatch`: weak. Tests cover the mock and argument builders only. There are no tests for `runJSON`, subprocess error handling, malformed JSON, or real `agent-mux` compatibility.
- `cmd/tickets`: moderate for the currently implemented command subset. Happy-path coverage exists for `init`, `create`, `show`, `list`, `dispatch`, `complete`, `fail`, `cancel`, `reopen`, `block`, and `delete --cascade`.

Missing or inadequate test areas:

- No tests for missing spec verbs because those verbs do not exist.
- No tests for `.tickets.toml` config, env/config precedence, or dispatch defaults.
- No tests for batch dispatch.
- No tests for dependency-cycle rejection or max dependency depth.
- No tests for cross-initiative dependencies.
- No tests for `migrate`, `reconcile`, `dispatch-ready`, `tick`, `board`, or `initiatives`.
- No tests for token population on completion/reconcile.
- No tests that `complete` rejects placeholder-only result content.
- No tests for delete refusal messaging that shows the dependency branch.
- No tests for malformed frontmatter from the command layer, only from the parser package.

## G. Code Quality

Issues found:

1. `cmd/tickets/dispatch_cmd.go` is not a real dispatch implementation. It writes fake `dispatch_id` values and leaves `session_id` unset, which breaks traceability and makes later reconcile/status flows impossible.
2. `cmd/tickets/create.go` and `cmd/tickets/init.go` write `created` timestamps in RFC3339 format, but the spec requires date-only strings.
3. `cmd/tickets/dispatch_cmd.go` accepts empty engine/model/effort and has no default resolution path, despite the spec requiring flags or config defaults for dispatch decisions.
4. `cmd/tickets/create.go` stores `depends_on` without any cycle detection, max-depth enforcement, or existence validation.
5. `cmd/tickets/complete.go` never records token usage, so every completed ticket will violate the intended completion contract when token data is available.
6. `frontmatter/write.go` re-marshals YAML, so the package does not preserve original header bytes, comments, or formatting. That may be acceptable for v1 implementation, but it is weaker than the spec’s stated round-trip bar.
7. `cmd/tickets/main.go` exposes an undocumented `edit` verb while omitting multiple required v5 verbs. That increases surface area without moving spec compliance.

Dead code / unused exports:

- The entire `dispatch` package is currently unused by the CLI.
- `trimSectionContent()` in `cmd/tickets/helpers.go` is unused.

Build / dependency status:

- `go test ./...` passes.
- `go vet ./...` produced no output.
- `go.mod` is minimal and sane; only `gopkg.in/yaml.v3` is required.

## H. Recommendations

1. Replace the current `cmdDispatch` stub with a real `agent-mux` integration that uses the existing `dispatch` package, records both `dispatch_id` and `session_id`, supports retry preamble injection, and resolves defaults from config.
2. Implement the missing required verbs: `board`, `initiatives`, `migrate`, `reconcile`, `dispatch-ready`, and `tick`. `reconcile` and `tick` are especially important because the current lifecycle has no recovery path for orphaned work.
3. Add a real config layer for `.tickets.toml` plus env precedence. Make `base_dir`, `agent_mux_bin`, `max_retry`, and `[defaults]` first-class instead of scattering hardcoded values.
4. Fix schema-format drift: write `created` as date-only, populate `tokens` on completion/reconcile, and enforce the full dispatch metadata lifecycle from the spec.
5. Add dependency validation on every `depends_on` write: existence checks, cycle detection, and max depth 3.
6. Upgrade JSON read output to include computed annotations so `show`, `list`, and the future `board`/`initiatives` commands satisfy the observability model in the spec.
7. Tighten tests around failure paths: malformed frontmatter at the CLI layer, invalid transitions for each verb, batch dispatch, token backfill, delete-with-dependents messaging, and byte-for-byte round-trip guarantees where required.
