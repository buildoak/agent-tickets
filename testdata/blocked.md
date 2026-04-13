---
id: TEST-005
initiative: TEST
title: "Blocked ticket for testing"
status: blocked
tier: worker
tags: [test]
created: 2026-04-06
manual: false
plan_ref: null
depends_on: []
dispatch_id: "01JQBLOCK001"
session_id: "sess-blk01"
engine: codex
model: gpt-5.4-mini
effort: xhigh
attempts: 3
last_attempt_outcome: failed
block_reason: "Auto-blocked after 3 failed attempts"
tokens: null
---

## Context
This ticket has been blocked after multiple failures.

## Scope
A task that repeatedly failed.

## Result

## Log
- 2026-04-06T10:00:00Z open -- created for testing
- 2026-04-06T10:05:00Z dispatched -- attempt 1
- 2026-04-06T10:15:00Z failed -- timeout
- 2026-04-06T10:20:00Z open -- reopened, attempts=1
- 2026-04-06T10:25:00Z dispatched -- attempt 2
- 2026-04-06T10:35:00Z failed -- timeout
- 2026-04-06T10:40:00Z open -- reopened, attempts=2
- 2026-04-06T10:45:00Z dispatched -- attempt 3
- 2026-04-06T10:55:00Z failed -- timeout
- 2026-04-06T11:00:00Z open -- reopened, attempts=3
- 2026-04-06T11:05:00Z dispatched -- attempt 4
- 2026-04-06T11:15:00Z blocked -- auto-blocked, attempts=3 >= max_retry=3
