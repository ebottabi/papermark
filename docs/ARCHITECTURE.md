# Architecture

## Overview

The Papermark agent server runs autonomous agent tasks for the Keen iOS client. A task is a prompt plus a
workspace directory. The agent loop calls Fireworks `/chat/completions` (GLM 5.3 Flash and DeepSeek V4.1 Flash), executes tools (files, shell, web,
connectors), and records every step as an event the client can replay and follow live. API.md is the HTTP
contract.

Two binaries share one codebase. `cmd/server` serves HTTP and runs a job worker in-process.
`cmd/worker` runs only the worker, so model runs can scale apart from HTTP. Both may run at once.

## Request lifecycle

```
client ── HTTP ──> Fiber (recover, logger, CORS, session auth ─> user id in Locals)
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
| `internal/harness` | Agent loop: `ChatTurn` builds a `turn` (model call, tools, store, repeat); owns the task event log |
| `internal/services/jobs` | Durable job queue: enqueue, claim, lease, retry, reclaim |
| `internal/workers` | Job handlers; `workers.NewRegistry` is what both binaries run |
| `internal/models` + `queries` | Schema (GORM tags, migration only) and raw SQL |
| `internal/cache` | Redis: cache-aside helpers and the live event bus |
| `internal/storage` | Local-disk storage and per-task workspaces |
| `internal/integrations` | Fireworks client, MCP OAuth |

## Users and sessions

Google sign-in (`services.GoogleVerifier`) verifies the ID token's RS256 signature against Google's JWKS,
cached for the response's `max-age`, and its `iss`, `aud` (`GOOGLE_CLIENT_IDS`), `exp` and `email_verified`.
It upserts the user by `google_sub`, which is the only way an account is created. Passkeys (go-webauthn,
discoverable, user verification required) and Face ID keys (Secure Enclave P-256, verified with the standard
library) are enrolled by a signed-in user and only sign in. WebAuthn ceremony state and Face ID challenges are
`auth_challenges` rows with a short TTL, taken once with `DELETE ... RETURNING`.

A sign-in issues a random 32-byte bearer token; `sessions` stores only its SHA-256, the user, `expires_at`
and `last_used_at`. `middleware.AuthMiddleware` resolves the token to a user id and stores it in Fiber Locals;
controllers read it with `middleware.UserID(c)` and pass it to every service call.

Every resource table has `user_id NOT NULL`, and every query filters by it, so another user's row is simply
not found (404). Background work carries the owner along: the harness loads the task owner's connectors
(`ConnectorProvider.Snapshot(ctx, userID)`), push goes to the owner's devices, the schedule ticker creates
tasks for the schedule's owner, a fork keeps its source's owner, and a connector's OAuth state lives on the
starting user's credential row.

The first boot that creates `users` truncates the existing data tables once (`models.PreMigrations`), since
pre-account rows have no owner and `user_id` is NOT NULL.

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
