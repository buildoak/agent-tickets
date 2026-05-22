# Initiatives Are Contracts

An initiative is more than a label. It is the reusable operating contract for a
kind of work.

The folder gives tickets a namespace. The initiative doc gives workers the
repeated context so individual cards can stay small.

```text
tickets/
  INITIATIVES/
    RESEARCH.md
    BUILD.md
    OPS.md
  cards/
    RESEARCH/
      RESEARCH-001.md
    BUILD/
      BUILD-001.md
```

## The Three Layers

**Config:** `.tickets.toml` defines runtime defaults: base directory,
dispatcher binary, retry policy, concurrency caps, and profile/model mappings.

**Initiative:** `INITIATIVES/<NAME>.md` explains what the workstream is, which
defaults apply, how workers should frame cards, and what good results look like.

**Card:** `cards/<NAME>/<ID>.md` is the actual work unit: context, scope, result,
and log.

This split keeps repeated instructions out of every ticket. It also makes the
tree readable with plain filesystem tools.

## When To Create An Initiative

Create an initiative when the work has a repeated workflow, durable context, or
its own quality gate.

Good initiatives:
- `RESEARCH` for source-backed scouting and briefs;
- `BUILD` for implementation tasks;
- `OPS` for maintenance and infrastructure work;
- `AUDIT` for review passes over completed work.

Weak initiatives:
- one initiative per random idea;
- one initiative per worker;
- one initiative per temporary mood;
- a duplicate of an existing workflow with a different name.

## What Belongs In The Initiative Doc

Use the initiative doc for material that would otherwise be pasted into every
card:

- objective and boundaries;
- default profile and skills;
- routing rules;
- scope pattern;
- done criteria;
- domain caveats;
- report format.

Use the card for the specific ask.

## Dependency Shape

Use `depends_on` when the downstream ticket needs successful upstream output.

Use `awaits` when the downstream ticket only needs upstream work to reach any
terminal state. This is useful for audits, summaries, cleanup passes, and batch
reports.

```yaml
depends_on: [BUILD-001]
awaits: [RESEARCH-001, RESEARCH-002]
```

The first means: do not dispatch unless `BUILD-001` is `done`.

The second means: dispatch once both research tickets are terminal, even if one
failed or blocked.

## Rule Of Thumb

If a worker would need the same paragraph on three cards, that paragraph belongs
in the initiative doc.

If the instruction only applies to one task, keep it in the card.
