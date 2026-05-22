---
initiative: EXAMPLE
title: "Example Initiative"
status: active
default_profile: ticket-worker
default_skills: []
---

## Objective

What kind of work belongs here, and why this initiative exists.

## Routing

Use this initiative when:
- the task matches this repeated workflow;
- the worker needs this domain context;
- the result should be grouped with related cards.

Do not use this initiative when:
- the work is one-off and better handled directly;
- another initiative already owns the domain;
- the user is still deciding the shape of the work.

## Scope Pattern

A good card in this initiative should include:
- the concrete ask;
- any files, sources, or prior ticket IDs the worker must read;
- the output format expected in `## Result`;
- a clear “Done means” gate.

## Done Criteria

A ticket in this initiative is acceptable when:
- `## Result` directly answers the scope;
- evidence is included for important claims;
- changed files or artifact paths are listed when relevant;
- blocked items and next steps are explicit.

## Conventions

- Keep repeated onboarding here, not in every ticket.
- Keep each ticket small enough for one worker pass.
- Prefer `depends_on` for required upstream output.
- Prefer `awaits` for audits, cleanup, or aggregation after a batch finishes.
