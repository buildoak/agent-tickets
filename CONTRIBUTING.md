# Contributing

agent-tickets is a small Go CLI. Keep changes focused, testable, and aligned
with the existing filesystem-first design.

## Development

```bash
go test ./...
go build -o tickets ./cmd/tickets
```

## Guidelines

- Keep ticket cards as the source of truth; do not add a database or service
  dependency without a clear design discussion.
- Route lifecycle changes through the FSM package.
- Preserve frontmatter round-trip behavior when changing parsing or writing.
- Add tests for new commands, state transitions, and user-facing behavior.
- Keep public examples generic. Do not commit local paths, personal
  automation, secrets, private LaunchAgents, or internal archive notes.

## Pull Requests

Before opening a pull request:

- run `go test ./...`,
- update public documentation for behavior changes,
- describe the user-visible change and any compatibility impact,
- keep unrelated cleanup out of the diff.
