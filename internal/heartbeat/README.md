# Heartbeat Package

The `heartbeat` package implements scheduled task execution for AgentLoop.

## Overview

The heartbeat system allows users to schedule skills to run automatically on a recurring basis (daily, weekly, every N hours, etc.). Scheduled tasks are stored per-user in SQLite databases and are periodically checked for execution.

## Packages

### `scheduled/`

Implements the scheduled task store and data model.

**Key types:**
- `ScheduledTask` — represents a task scheduled to run (ID, Name, Schedule, SkillPath, Prompt, Enabled, timestamps)
- `TaskStore` interface — defines the storage contract (CRUD + ListDue)
- `SQLiteTaskStore` — production implementation using per-user SQLite databases at `vault/heartbeat/scheduled/{userId}.db`
- `InMemoryTaskStore` — test-only implementation

**Key methods:**
- `Add(task)` — create a new scheduled task
- `GetByID(userID, id)` — retrieve a task by ID
- `List(userID)` — list all tasks for a user
- `ListDue(userID, now)` — list tasks due for execution (enabled and NextRunAt <= now)
- `UpdateLastRunByID(userID, id, lastRunAt, scheduleString)` — update run time and compute next run
- `DisableByID(userID, id)` — disable a task without deleting it
- `DeleteByID(userID, id)` — delete a task

## Data Storage

Scheduled tasks are stored in per-user SQLite databases following the same pattern as `internal/memory/notes/sqlite.go`:

- **Location**: `~/.local/share/agentloop/vault/heartbeat/scheduled/{userId}.db`
- **Schema**: Single `scheduled_tasks` table with indices on user_id and next_run_at
- **Lazy open**: User database is opened on first access; all operations are mutex-protected
- **Migration**: Schema creation is idempotent

## Schedule Computation

The `computeNextRun()` function calculates the next execution time from a schedule string. Currently supports:
- Natural language patterns: "daily", "every 2h", etc.
- Future: Full cron expression support via `robfig/cron`

## Testing

All tests are in `scheduled_test.go`:
- `TestScheduledTaskCRUD` — basic create/read/update/delete operations (in-memory)
- `TestListDueFiltering` — verifies ListDue filters by enabled status and due time (in-memory)
- `TestDisableTask` — verifies disabling a task (in-memory)
- `TestSQLiteTaskStoreCRUD` — CRUD operations on SQLite backend
- `TestSQLiteListDueFiltering` — ListDue filtering on SQLite backend
- `TestSQLiteDisableTask` — disabling on SQLite backend
- `TestSQLiteUpdateLastRun` — updating last run and computing next run
- `TestSQLiteDelete` — deletion on SQLite backend
- `TestInMemoryTaskStore` — in-memory store conformance

Run all tests:
```bash
go test ./internal/heartbeat/scheduled/... -v
```

## Integration

The heartbeat system is designed to be integrated with:
- **Agent Core**: Tasks will be fetched by the heartbeat scheduler and executed as skills
- **Memory Engine**: Task outcomes will be recorded as interactions for memory and evolution
- **Skills Registry**: Each task references an absolute skill path that is loaded at execution time
