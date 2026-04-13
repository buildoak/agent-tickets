# agent-tickets v5 Deep Audit (codex)

## Executive Summary
This codebase is not spec-complete. The CLI surface mostly exists, but several core contracts are violated: frontmatter round-trip fidelity is not preserved, dispatch does not enforce the required `## Scope` gate, reconcile cannot record agent-mux failure details, and delete can remove dispatched descendants during cascade. Config handling also misses the spec's hardcoded `base_dir` default, and the dispatch adapter diverges from the documented agent-mux contract by relying on a non-spec `--preamble` flag and by not guaranteeing an absolute ticket path. Confidence in these findings is high: I read the full spec first, then all Go code and tests, and `go vet ./...` plus `go test ./...` both passed.

## Verb-by-Verb Drill

| Verb | Status | Audit |
| --- | --- | --- |
| `init` | PARTIAL | Creates directory and `INITIATIVE.md`, errors if initiative dir exists, and scaffolds the required sections. It still depends on explicit `--base` / `TICKETS_BASE_DIR` because `config.Load()` never applies the spec's hardcoded `base_dir = "centerpiece/tickets"` default (`config/config.go:30-57`, `cmd/tickets/helpers.go:22-53`). |
| `create` | PARTIAL | Correct flags exist and dependency existence / cycle / depth checks are implemented (`cmd/tickets/create.go:14-73`, `cmd/tickets/helpers.go:296-389`). It does not initialize the in-memory `tags` field to the spec-mandated empty slice, and there is no follow-up validation path for manual `depends_on` edits despite the spec explicitly requiring it. |
| `show` | PASS | Supports `--json`, returns the card, and JSON includes computed board annotation/detail (`cmd/tickets/show.go:10-58`). Text mode simply reprints the card bytes. |
| `list` | PASS | Supports `--initiative`, `--status`, `--tag`, `--json`, and JSON entries include computed annotations (`cmd/tickets/list.go:10-85`). Output behavior is consistent with the spec's read-verb contract. |
| `board` | PARTIAL | Supports `--initiative` and `--json`, and computes `queued` / `waiting` / `manual` / `running` / `done` / `blocked` annotations (`cmd/tickets/board.go:18-134`). Failed tickets do not show the last error as required by spec Section 4; they only show attempts plus `last_attempt_outcome`, which is usually just `"failed"` (`cmd/tickets/board.go:109-117`). |
| `initiatives` | PARTIAL | Supports `--status` and `--json`, and counts tickets per initiative (`cmd/tickets/initiatives.go:21-84`). The usage string and filter vocabulary diverge from the spec (`completed|archived` vs spec `complete`), and the initiative-card parser is brittle because it splits on raw `---` bytes instead of using the frontmatter parser (`cmd/tickets/initiatives.go:31-33`, `cmd/tickets/initiatives.go:87-101`). |
| `dispatch` | FAIL | Batch form, default resolution, dependency checks, and frozen dispatch metadata exist (`cmd/tickets/dispatch_cmd.go:28-157`). It violates the spec in multiple ways: it never enforces the mandatory non-empty `## Scope` gate before dispatch, it does not guarantee an absolute ticket path, it allows empty profile even though the spec contract always dispatches with a profile, and it depends on a non-spec `agent-mux --preamble` flag (`cmd/tickets/dispatch_cmd.go:105-135`, `dispatch/shell.go:28-50`). |
| `complete` | FAIL | Correctly enforces `dispatched -> done` and populates tokens when available (`cmd/tickets/complete.go:36-73`). It adds an extra placeholder-content gate not present in v5, uses a different error message than the spec's required completion error, and fails to append the required token summary to the log (`cmd/tickets/complete.go:43-49`, `cmd/tickets/complete.go:72`). |
| `fail` | PARTIAL | Correct state guard and `--reason` flag exist (`cmd/tickets/fail.go:12-75`). Problems: config load errors are silently discarded, reconcile cannot pass through actual agent-mux error details, and auto-block is implemented as `failed` then `blocked` rather than directly blocking on the terminal fail event described by the spec (`cmd/tickets/fail.go:57-73`). |
| `cancel` | PASS | Correctly restricts to dispatched tickets, preserves partial result content, preserves dispatch fields, and sets `last_attempt_outcome = cancelled` (`cmd/tickets/cancel.go:11-54`). This matches the spec. |
| `reopen` | PARTIAL | The valid transitions are enforced and attempts increment only on reopen-from-failed (`cmd/tickets/reopen.go:11-55`, `fsm/fsm.go:58-81`). The implementation does archive previous `## Result` on reopen-from-done, but it does so in an ad hoc freeform log block rather than a structured archive entry, and `block_reason` is only explicitly cleared on reopen-from-blocked even though Section 3 says reopen resets it generically (`cmd/tickets/reopen.go:46-53`, `fsm/fsm.go:69-81`). |
| `block` | PASS | Correct flags and allowed source states (`open`, `failed`) are enforced, and `block_reason` is recorded (`cmd/tickets/block.go:11-52`). This matches the state diagram. |
| `delete` | FAIL | The base refusal-on-dependents flow and cascade prompt are implemented (`cmd/tickets/delete_cmd.go:46-65`). It fatally violates the spec by only checking the root ticket for `dispatched` status; `--cascade` can delete dependent tickets that are themselves `dispatched`, orphaning live agents (`cmd/tickets/delete_cmd.go:38-44`, `cmd/tickets/delete_cmd.go:67-83`). |
| `migrate` | FAIL | It moves the file, updates `initiative` / `id`, resequences, and rewrites `depends_on` references across tickets (`cmd/tickets/migrate.go:48-131`). It omits the spec's "refuse if update scope is too large" safeguard, does not validate dependency constraints after rewriting references, and does not check whether the rewritten branch includes dispatched tickets that should not be mutated casually. |
| `reconcile` | FAIL | It scans dispatched tickets, consults agent-mux status, transitions completed/failed/timeouts, and backfills missing tokens on done cards (`cmd/tickets/reconcile.go:35-177`). It still diverges from spec: status-query errors only print "orphan" text and leave the card untouched, completed-without-result uses a different failure reason than the spec text, and agent-mux failures cannot append backend error details because the status model has no error field (`cmd/tickets/reconcile.go:59-63`, `cmd/tickets/reconcile.go:115-148`, `dispatch/dispatch.go:9-20`). |
| `dispatch-ready` | PARTIAL | Uses defaults from `.tickets.toml`, skips manual tickets, respects dependency readiness, and sorts by `created` ascending (`cmd/tickets/dispatch_ready.go:10-116`). It aborts the whole pass on the first dispatch failure instead of dispatching each eligible ticket independently, which is weaker than the spec's general "each ticket independently" dispatch behavior. |
| `tick` | PARTIAL | Acquires `.tick.lock`, exits silently if locked, runs reconcile then dispatch-ready, and prints a summary (`cmd/tickets/tick.go:10-41`). The lock is a bare create/delete sentinel rather than a robust lease; stale lock files will wedge the scheduler indefinitely. |

Non-spec surface:
- `edit` exists even though it is not one of the 17 spec verbs (`cmd/tickets/main.go:57-62`, `cmd/tickets/edit.go:10-48`). Worse, it directly opens the file and performs no post-edit dependency validation, violating the spec's requirement that manual `depends_on` edits detected by the CLI must be cycle-checked.

## Data Model

| Field | Spec | Code | Assessment |
| --- | --- | --- | --- |
| `id` | `string` | `string` | PASS |
| `initiative` | `string` | `string` | PASS |
| `title` | `string` | `string` | PASS |
| `status` | `Status` enum | `Status` enum | PASS |
| `tier` | `Tier` enum | `Tier` enum | PASS |
| `tags` | `[]string`, empty slice if none | `[]string` | PARTIAL. Type is right, but `Parse` leaves it nil when absent/null; only `Serialize` normalizes nil to `[]` (`frontmatter/card.go:28-53`, `frontmatter/parse.go:16-30`, `frontmatter/write.go:10-17`). |
| `created` | `string` date-only | `string` | PASS |
| `manual` | `bool` | `bool` | PASS |
| `plan_ref` | `*string` | `*string` | PASS |
| `depends_on` | `[]string`, empty slice if none | `[]string` | PARTIAL. Same nil-vs-empty problem as `tags` (`frontmatter/card.go:40`, `frontmatter/parse.go:22-30`, `frontmatter/write.go:15-17`). |
| `dispatch_id` | `*string` | `*string` | PASS |
| `session_id` | `*string` | `*string` | PASS |
| `engine` | `*string` | `*string` | PASS |
| `model` | `*string` | `*string` | PASS |
| `effort` | `*string` | `*string` | PASS |
| `attempts` | `int` | `int` | PASS |
| `last_attempt_outcome` | `*string` | `*string` | PASS |
| `block_reason` | `*string` | `*string` | PASS |
| `tokens` | `*TokenUsage` | `*TokenUsage` | PASS |

Frontmatter fidelity:
- FAIL. The spec requires frontmatter round-trip fidelity for non-mutated fields; `Serialize()` re-marshals the entire header through `yaml.Marshal`, destroying original field order, quoting, comments, and exact scalar formatting (`frontmatter/write.go:10-35`).
- PASS for body-byte preservation on pure frontmatter mutations: the raw body bytes are copied on parse and concatenated unchanged on serialize (`frontmatter/parse.go:27-30`, `frontmatter/write.go:24-35`).
- PARTIAL for section mutation correctness: section APIs work for simple `## Name` files, but matching is substring-based and can hit `## Logbook` when asked for `## Log`, and next-section detection only looks for `\n## `, so CRLF section boundaries are mishandled (`frontmatter/sections.go:41-70`).

## FSM

| Transition | Spec | Code | Assessment |
| --- | --- | --- | --- |
| `open -> dispatched` | `dispatch`, freeze snapshot, enforce dispatch guards | Implemented in FSM and CLI | PARTIAL. Transition exists, but CLI misses the mandatory `## Scope` gate and absolute-path dispatch contract (`fsm/fsm.go:39-45`, `cmd/tickets/dispatch_cmd.go:105-135`). |
| `open -> blocked` | `block` | Implemented | PASS |
| `dispatched -> done` | `complete`, result non-empty, token capture, log summary | Implemented | PARTIAL. Extra placeholder gate and missing token-summary log (`cmd/tickets/complete.go:43-72`). |
| `dispatched -> failed` | `fail` / reconcile, log real reason | Implemented | PARTIAL. Transition exists, but real backend error details are not persisted (`cmd/tickets/fail.go:49-75`, `cmd/tickets/reconcile.go:139-167`). |
| `dispatched -> open` | `cancel`, preserve dispatch fields, no attempt increment | Implemented | PASS |
| `failed -> open` | `reopen`, attempts++ | Implemented | PASS |
| `failed -> blocked` | manual block or auto-block on fail threshold | Implemented | PARTIAL. Auto-block is layered after entering `failed` rather than being the direct fail outcome described in Section 15 (`cmd/tickets/fail.go:64-73`). |
| `blocked -> open` | `reopen`, clear block reason | Implemented | PASS |
| `done -> open` | `reopen`, archive previous result | Implemented | PARTIAL. Result is archived, but formatting is freeform and there is no explicit trace structure (`cmd/tickets/reopen.go:46-53`). |

Extra / missing transitions:
- No extra FSM transitions were found in `fsm/fsm.go`.
- No spec transition is missing from the FSM table.
- The CLI adds non-spec verb `edit`, which bypasses state/data integrity checks rather than fitting the FSM.

## Config & Dispatch

`.tickets.toml` schema is close, but the config layer is incomplete. `agent_mux_bin` and `max_retry` defaults exist, while the spec's hardcoded `base_dir = "centerpiece/tickets"` default is missing entirely (`config/config.go:11-57`). Precedence is only partially implemented because only `TICKETS_BASE_DIR` and `TICKETS_AGENT_MUX_BIN` can override file config; there is no hardcoded `base_dir` fallback, so many commands fail unless base dir is provided explicitly (`cmd/tickets/helpers.go:42-53`).

Dispatch contract compliance is weak:
- The adapter uses `agent-mux dispatch --prompt-file ... [--profile ...] [--engine ...] [--model ...] [--effort ...] [--preamble ...]` (`dispatch/shell.go:28-50`).
- The spec contract only documents `--profile PROFILE --prompt-file FILE [--engine E --model M --effort X]`; `--preamble` is a private extension, so this implementation is coupled to an undocumented backend interface.
- `DispatchOptions.TicketPath` is documented as absolute, but `dispatchTicket()` passes through whatever `findTicketFile()` returns; if `base_dir` is relative, the prompt file is relative too (`dispatch/dispatch.go:22-30`, `cmd/tickets/helpers.go:68-97`, `cmd/tickets/dispatch_cmd.go:128-135`).
- `StatusResult` has no field for backend error details, so reconcile cannot satisfy the spec's requirement to append the actual agent-mux failure reason (`dispatch/dispatch.go:9-20`, `cmd/tickets/reconcile.go:139-167`).

## Bugs Found

1. `tickets delete --cascade` can delete dispatched descendants, orphaning live agents. The code only checks the root ticket's status before recursively removing every target file. File refs: `cmd/tickets/delete_cmd.go:38`, `cmd/tickets/delete_cmd.go:67`, `cmd/tickets/delete_cmd.go:81`.
2. Frontmatter writes are not spec-fidelitous. `Serialize()` re-marshals the full YAML header, so comments, order, quoting, and exact scalar forms are lost after any mutation. File refs: `frontmatter/write.go:10`, `frontmatter/write.go:19`.
3. Dispatch can launch tickets with an empty `## Scope`, even though the spec says scope is mandatory before dispatch. File refs: `cmd/tickets/dispatch_cmd.go:105`, `cmd/tickets/dispatch_cmd.go:128`.
4. Dispatch does not guarantee the absolute prompt-file path required by the agent contract, and it relies on a non-spec `--preamble` backend flag. File refs: `cmd/tickets/helpers.go:74`, `cmd/tickets/dispatch_cmd.go:133`, `dispatch/shell.go:46`.
5. Reconcile cannot append actual backend failure reasons because the status model lacks an error field; terminal failures are reduced to generic canned text. File refs: `dispatch/dispatch.go:10`, `cmd/tickets/reconcile.go:143`, `cmd/tickets/reconcile.go:154`.
6. Reconcile treats agent-mux status-query errors as printable "orphan" notices and leaves tickets stuck in `dispatched`, which defeats the defensive-cleanup purpose of reconcile. File refs: `cmd/tickets/reconcile.go:59-63`.
7. Section lookup is unsafe: `GetSection("Log")` will match `## Logbook`, and CRLF section separators are not recognized. File refs: `frontmatter/sections.go:42-47`, `frontmatter/sections.go:64`.
8. `fail` swallows config-load errors and silently falls back to default retry behavior. A malformed or unreadable `.tickets.toml` changes lifecycle behavior without surfacing an error. File refs: `cmd/tickets/fail.go:59-63`.
9. Manual editing bypasses dependency validation entirely. The non-spec `edit` verb opens the card in `$EDITOR` and does not re-validate `depends_on`, despite the spec explicitly requiring cycle detection on manual edits detected by the CLI. File refs: `cmd/tickets/edit.go:10-48`.
10. Scheduler locking is vulnerable to stale lock files. `tick` uses `os.O_EXCL` on a plain file and never records PID / age / lease metadata, so a crash leaves the scheduler permanently wedged until manual cleanup. File refs: `cmd/tickets/tick.go:24-41`.
11. All writes are non-atomic `os.WriteFile` replacements with no per-ticket locking, so concurrent commands can race and lose updates. File refs: `frontmatter/write.go:38-44`.
12. Board output for failed tickets is misleading because it displays `last_attempt_outcome` instead of the actual error. For most failures the detail line becomes effectively `attempts N: failed`, which carries no diagnostic value. File refs: `cmd/tickets/board.go:109-117`.

## Code Quality Issues

1. Tests mostly verify the implementation against itself rather than against the spec. There is no test for the mandatory `## Scope` dispatch gate, no test that prompt paths are absolute, no test that cascade delete refuses dispatched descendants, and no test that manual edits re-run dependency validation.
2. The frontmatter round-trip test only checks body equality and parsed struct equality; it does not verify header-byte fidelity, which is the critical spec gate for Session 1 (`frontmatter/frontmatter_test.go:20-41`, `frontmatter/frontmatter_test.go:44-74`).
3. Error handling is inconsistent. Some commands surface config errors, while `fail` discards them; `complete` silently ignores agent-mux token lookup failures; `reconcile` prints orphan text instead of returning actionable errors.
4. The CLI surface is inconsistent with the spec because it adds `edit` but omits any formal post-edit validation path.
5. Initiative parsing duplicates frontmatter logic instead of reusing the parser, increasing drift risk (`cmd/tickets/initiatives.go:87-101`).

## Verdict
This implementation is not spec-complete and should not be treated as production-ready v5. The highest-priority fixes before production use are: preserve frontmatter header fidelity, enforce the dispatch `## Scope` gate, make delete/cascade safe with dispatched descendants, align the dispatch adapter with the documented agent-mux contract, and carry real failure detail through reconcile and board output. After those are fixed, the next tier is hardening config defaults, post-edit dependency validation, and atomic write / locking behavior.
