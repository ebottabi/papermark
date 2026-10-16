# Architecture

## Overview

The Papermark agent server runs autonomous agent tasks for the Keen iOS client. A task is a prompt plus a
workspace directory. The agent loop calls the Anthropic Messages API, executes tools (files, shell, web,
connectors), and records every step as an event the client can replay and follow live. API.md is the HTTP
contract.

Two binaries share one codebase. `cmd/server` serves HTTP and runs a job worker in-process.
`cmd/worker` runs only the worker, so model runs can scale apart from HTTP. Both may run at once.

## Request lifecycle

```
client ── HTTP ──> Fiber (recover, logger, CORS, bearer auth)
                     └─> router ─> controller ─> service ─┬─> queries ─> Postgres
                                                          └─> jobs.Enqueue (same tx)
worker ── claim (FOR UPDATE SKIP LOCKED) ─> job handler ─> agent run
             └─ events: Postgres (durable) + Redis pub/sub (live, deltas included)
client <── SSE ── replay from Postgres after Last-Event-ID, then follow Redis
```

## Components

| Package | Role |
|---------|------|
| `internal/router` | Builds the Fiber app, `/health`, `/ready`, every `/v1` route |
| `internal/controllers` | Parse and validate requests, call services, send envelopes |
| `internal/services` | Business logic and transactions; `services.New` wires them all |
| `internal/services/jobs` | Durable job queue: enqueue, claim, lease, retry, reclaim |
| `internal/workers` | Job handlers; `workers.NewRegistry` is what both binaries run |
| `internal/models` + `queries` | Schema (GORM tags, migration only) and raw SQL |
| `internal/cache` | Redis: cache-aside helpers and the live event bus |
| `internal/storage` | Local-disk storage and per-task workspaces |
| `internal/integrations` | Anthropic client and the scripted fake |

## Data layer

Postgres is the source of truth. A run never holds state only in memory: the model conversation, pending
approvals and partial tool results are rows. A worker that dies mid-run loses only its lease; another
worker reclaims the job and continues from the saved conversation.

## Live events

Each event is appended to Postgres with a per-task sequence number and then published to Redis channel
`papermark:task:<id>`. Streaming text deltas are published only. Without Redis (single process), an
in-process bus does the same job.

## Workspaces

Each task's files live in `WORKSPACE_DIR/<task id>`. Server and workers must share that directory
(same host, or a shared volume).
