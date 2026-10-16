# Papermark Agent Server

A Go service that runs long-lived agent tasks against the Anthropic Messages
API. Each task runs as a durable job in Postgres, so a restart or a crashed
worker resumes it instead of losing it. Live progress is published on Redis
and streamed to clients over Server-Sent Events. The layout follows bee-saas;
see docs/ARCHITECTURE.md and API.md.

## Run

Postgres and Redis must be running.

```sh
cp .env.example .env   # or export the variables below
make run               # builds bin/server and starts it on 127.0.0.1:8787
```

`DATABASE_URL`, `REDIS_URL`, `API_TOKEN` and `ANTHROPIC_API_KEY` are the
variables a deployment sets. `WORKSPACE_DIR` holds the per-task files.
`WORKER_CONCURRENCY`, `WORKER_LEASE_SECONDS` and `WORKER_POLL_MS` tune the job
worker. `make run-worker` starts a worker without the HTTP API.

## Without an API key

`make run-fake` starts a scripted stand-in for the Messages API on :8790 that
plays testdata/lisbon.json. Point the server at it:

```sh
ANTHROPIC_BASE_URL=http://127.0.0.1:8790 ANTHROPIC_API_KEY=fake API_TOKEN=dev make run
curl -H 'Authorization: Bearer dev' -d '{"task":"Plan a weekend in Lisbon"}' \
  -H 'Content-Type: application/json' http://127.0.0.1:8787/v1/tasks
```

## Test

```sh
make test-race
```

The tests create throwaway databases on `TEST_DATABASE_URL` (default: the
local server as the current OS user) and use `TEST_REDIS_URL` (default
redis://localhost:6379/0). The HTTP tests in internal/router build cmd/server
and run it as a real process, killing and restarting it to check recovery.
