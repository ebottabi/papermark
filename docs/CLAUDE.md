# Papermark Agent Server Coding Guidelines

The layout and conventions follow bee-saas. When in doubt, do what bee-saas does.

## Tech Stack (STRICT)
- **Go 1.26** for everything
- **Fiber v2** for HTTP routing and middleware
- **PostgreSQL** is the source of truth (users, sessions, tasks, events, model messages, jobs, projects, knowledge, skills)
- **pgx / pgxpool** runs ALL runtime queries as raw SQL
- **Redis** carries live task events between the worker and the HTTP servers
- **GORM** for migrations ONLY (`AutoMigrate`). Never used to query/read/write data.
- **Fireworks** `/chat/completions` (streaming) through `internal/integrations/fireworks`, no SDK

### Data access rule (NON-NEGOTIABLE)
- GORM exists solely to migrate the schema (it reads the struct tags on the models).
- Every read/write at runtime is **raw SQL** through the **pgx pool**.
- SQL lives in `internal/models/queries`, **one file per model**, each function taking a `DBTX`
  (satisfied by `*pgxpool.Pool` and `pgx.Tx`).

---

## Project Structure

```
papermark/
├── cmd/
│   ├── server/main.go           # HTTP API + in-process job worker; migrates on boot
│   └── worker/main.go           # Standalone job worker
├── internal/
│   ├── config/                  # Configuration loading
│   ├── models/                  # Entity structs (GORM tags drive migration only)
│   │   └── queries/             # Raw SQL per model (queries/task.go, ...)
│   ├── controllers/             # HTTP handlers
│   ├── handlers/                # Shared handler scaffolding (envelope, Send*)
│   ├── responses/               # Response DTOs (exact API.md shapes)
│   ├── router/                  # Fiber app + route definitions
│   ├── middleware/              # Session auth (user id in Locals), logging, ratelimit
│   ├── harness/                 # Agent loop: harness.go (ChatTurn), turn.go, messages.go, prompt.go,
│   │   │                        #   preview.go, connectors.go, events.go (task event log)
│   │   ├── tools/               # Built-in tools, one file each (tool.go: Tool, Env, Offered; web.go)
│   │   └── skills/              # Built-in skills, one Markdown file each, embedded
│   ├── services/                # Business logic
│   │   └── jobs/                # Durable job queue (outbox) + worker loop
│   ├── workers/                 # Job handlers + the registry both binaries run
│   ├── integrations/            # External APIs (fireworks, mcp)
│   ├── cache/                   # Redis access: cache-aside + live event bus
│   ├── storage/                 # Local-disk storage + task workspaces
│   ├── database/                # GORM migrator + pgx pool + Redis client + test databases
│   └── utils/                   # Shared utilities
├── deploy/                      # systemd units + env example
├── docs/                        # Project documentation
└── go.mod
```

---

## Copyright Header (REQUIRED)

Every Go file MUST start with this copyright header:

```go
/*
 * Copyright (c) 2026. Papermark - All Rights Reserved
 * Unauthorized copying of this file, via any medium is strictly prohibited
 * Proprietary and confidential
 * Written by Ebot Tabi <ebot.tabi@gmail.com>, DD/MM/YYYY
 */
```

---

## End-to-End Feature Implementation Flow

Use the full flow for a new table or a new resource. For a method on existing code, add it where its siblings live.

1. **Model** (`internal/models/<entity>.go`). GORM tags only, no query methods. Embed `Base` for mutable,
   soft-deletable entities. Append-only tables (events, messages, jobs) own their keys and never soft-delete.
2. **Register** it in `models.AllModels()` (`internal/models/base.go`). Partial or expression indexes
   that tags cannot express go in `models.PostMigrations()`.
3. **Queries** (`internal/models/queries/<entity>.go`). Parameterized SQL in `const` blocks, functions
   taking `(ctx, db DBTX, ...)`, `pgx.ErrNoRows` mapped through `notFound`.
4. **Response DTO** (`internal/responses/<entity>.go`). Field names exactly as in API.md; times through
   `responses.Time` (RFC 3339, milliseconds, UTC).
5. **Service** (`internal/services/<entity>.go`). Holds the pool; opens a `pgx.Tx` for multi-statement
   work and passes it to the queries. Add it to `services.Services` and `services.New`.
6. **Controller** (`internal/controllers/<entity>.go`). Embeds `*handlers.BaseHandler`, parses and
   validates the request, calls the service, replies with `SendSuccess` / `SendCreated` / `SendBadRequest` ...
7. **Routes** (`internal/router/router.go`). Add the controller to `router.Config` and a
   `setup<Entity>Routes(api fiber.Router, cfg *Config)` that returns early on a nil controller.
8. **Background work** goes through `services/jobs`: enqueue in the same transaction as the state change,
   call `Notify()` after commit, and register the handler in `workers.NewRegistry`.

---

## Response envelope

Every JSON response goes through `handlers.BaseHandler`:

```
success: {"success": true, "data": <result>, "message"?: "..."}
error:   {"success": false, "error": "<message>", "code": "BAD_REQUEST" | "UNAUTHORIZED" | ...}
```

SSE streams, file downloads and the markdown transcript are raw. Unhandled errors reach the Fiber
`ErrorHandler` in `internal/router`, which maps the status to a code with `handlers.CodeForStatus`.

---

## Background jobs

- One table, `jobs`. A non-empty `key` (for example a task id) has at most one active job.
  Enqueueing an active key is a no-op, or flags the running job to run once more.
- Handlers must be idempotent: a crashed worker's job is reclaimed when its lease expires and runs again.
- Handler errors retry with `jobs.Backoff`; after `max_attempts` claims the job is `dead`.

---

## Core Principles

- Accept interfaces, return structs
- Errors are values; handle them explicitly
- Pass `context.Context` as the first argument to anything doing I/O
- Keep interfaces small
- Keep it simple; if a simple solution works, use it

## Naming Conventions

| Type | Pattern | Example |
|------|---------|---------|
| Model | `<Name>` | `Task`, `Project` |
| Controller | `<Name>Controller` | `TaskController` |
| Service | `<Name>Service` | `TaskService` |
| Response DTO | `<Name>DTO` | `TaskSummaryDTO` |
| Request struct | `<Action><Name>Request` | `CreateTaskRequest` |

Files are lowercase with underscores, one primary type per file, tests in `<name>_test.go`.

## Database Conventions

- Per-user data: every resource row has `user_id`, and every query takes the user and filters by it.
  Controllers pass `middleware.UserID(c)`; another user's row is `ErrNotFound` (404, never 403).
- UUID ids (`gorm:"type:uuid"`), set in Go on insert along with `created_at`/`updated_at`.
- Soft-deletable tables: every SELECT filters `deleted_at IS NULL`; delete with `UPDATE ... SET deleted_at`.
- The GORM handle is opened only to run `AutoMigrate`, then closed.

## Tests

Tests run against real Postgres and Redis. `database.NewTestDatabase(t)` creates a migrated throwaway
database and drops it on cleanup; `database.NewTestRedis(t)` connects to the shared Redis.
Run `go test -race ./...`. No test calls Fireworks and there is no fake model: the loop's pure parts
(stuck guard, trimming, argument parsing, fallback) are unit-tested and tasks run with an empty key.

## Prohibited Practices

- NO GORM at runtime; NO SQL string concatenation
- NO query logic in `internal/models/<entity>.go`
- NO `*pgxpool.Pool` access from controllers; go through a service
- NO `*redis.Client` access from services; go through `internal/cache`
- NO hardcoded configuration; use environment variables (`internal/config`)
- NO missing copyright headers
- NO `init()` functions unless absolutely necessary
