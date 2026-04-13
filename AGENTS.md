# AGENTS.md — agent-tickets

Read `CLAUDE.md` first. It has architecture, build/test, invariants, and conventions.

## Rules for Working on This Codebase

1. **Docs stay in sync with code.** Every logic change — new command, new state, changed behavior, new config field — must be reflected in:
   - `README.md` (public reference)
   - `CLAUDE.md` (architecture and conventions)
   - `coordinator-skill/SKILL.md` (coordinator playbook)

2. **Tests are mandatory for new command work.** All new commands should ship with test coverage in `main_test.go` using the `runCmd()` harness. Existing coverage is not yet universal.

3. **FSM first.** State changes start in `fsm/fsm.go`, not in command files. The FSM is the source of truth for lifecycle rules.

4. **Preserve round-trip fidelity.** Changes to `frontmatter/` must not break byte-exact YAML round-trip. Run `go test ./frontmatter/...` after any parse/write changes.

5. **Keep it lean.** No new dependencies without justification. No frameworks, no HTTP servers, no databases. This is a CLI tool that reads and writes markdown files.
