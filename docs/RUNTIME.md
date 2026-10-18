# Runtime: server, worker and triggers

In production the server only serves the API. Every chat turn, time routine and trigger runs in the worker.
Both processes share one Postgres database, one Redis and one workspace directory.

| Process | systemd unit | Runs | Does not run |
| --- | --- | --- | --- |
| `cmd/server` | `papermark.service` | HTTP API, auth, SSE streams, enqueueing jobs | jobs, schedules, triggers (`SERVER_BACKGROUND=off`) |
| `cmd/worker` | `papermark-worker.service` | job worker (concurrency 16), schedule ticker, mail and watch pollers | HTTP |

Without `SERVER_BACKGROUND=off` the server runs everything itself, so one binary is a complete system for
local development.

## The whole setup

```
                    ┌──────────────────────────┐
                    │      Keen iOS app        │
                    └──┬────────────────────▲──┘
         HTTPS request │                    │ SSE (events, live text)
                       ▼                    │
              ┌───────────────────────────────────┐
              │ nginx (TLS, proxy, no buffering)  │
              └─────────────────┬─────────────────┘
                                ▼ 127.0.0.1:8787
┌──────────────────── cmd/server (API only) ────────────────────┐
│ Fiber ─ session auth ─ router ─ controllers ─ services        │
│                                                               │
│   POST message ──► one tx: insert task_messages + jobs row    │
│   GET  events  ──► replay task_events, then follow Redis      │
│   POST stop    ──► set stop flag + publish stop frame         │
└──────────┬──────────────────────────────────────────▲─────────┘
           │ write (same tx)                          │ subscribe
           ▼                                          │
┌─────────────────────────────────────┐   ┌───────────┴──────────┐
│ Postgres                            │   │ Redis (pub/sub only) │
│  jobs           (the queue)         │   │  papermark:task:<id> │
│  tasks, task_messages, task_events  │   │  events + stop frames│
│  schedules, updates, memories       │   └───────────▲──────────┘
│  connectors, gmail_accounts,        │               │ publish
│  oauth_credentials                  │               │
│  mailbox_polls, watch_targets,      │               │
│  watch_subscriptions, trigger_events│               │
│  contacts, contact_syncs            │               │
└──────────┬──────────────────────────┘               │
           │ claim (FOR UPDATE SKIP LOCKED, lease)    │
           ▼                                          │
┌──────────────────── cmd/worker ───────────────────────────────┐
│ job worker, polls every 1s ── chat.turn ──► harness.ChatTurn  │
│                                   │ model API, tools,         │
│                                   │ Gmail, web, shell         │
│                                   └─► task_events + publish ──┘
│                                                               │
│ schedule ticker ── due time routine ──► task + chat.turn job  │
│                                                               │
│ trigger loop, every 5s                                        │
│   mail:  Gmail history.list per mailbox, every 60s + jitter   │
│   watch: fetch each due page or search once via safehttp      │
│   match ──► trigger_events (once per source) ──► task + job   │
│                                     or a direct update        │
│   contacts: People API sync per account, every 3h, own 30s    │
│             ticker so a long sync never delays mail           │
└───────────────────────────────┬───────────────────────────────┘
                                ▼
                     WORKSPACE_DIR/<task id>
```

## How a chat turn flows

The server writes the message and the job in one transaction. The worker claims the job and streams events
back through Redis.

1. The app sends a message. `TaskService` saves it and inserts a `chat.turn` job in the same transaction
   (`internal/services/task.go:128`, and `:325` for follow-ups).
2. The worker polls `jobs` every second (`WORKER_POLL_MS`). It claims the job with `FOR UPDATE SKIP LOCKED`
   and holds a lease that a heartbeat extends.
3. The worker runs `harness.ChatTurn` (`internal/workers/chat_turn.go:27`). That is the model loop and every
   tool call.
4. Each event is written to `task_events` and published on Redis channel `papermark:task:<id>`.
5. The server's SSE handler replays `task_events` from Postgres, then follows the Redis channel
   (`internal/controllers/task.go:308`).
6. When the agent asks a question or needs approval, the task parks and the job ends. The answer enqueues a
   new turn. An approved call runs the exact input shown on the confirm card.
7. Stop sets a flag in the database and publishes a stop frame (`internal/services/task.go:391`). The worker
   cancels the turn when it sees the frame (`internal/harness/harness.go:103`).

A job key allows one active turn per task. A second message during a turn flags the job to run once more.

## Why the queue is Postgres, not Redis

The job is inserted in the same transaction as the user's message, so both commit or neither does. A Redis
queue would need two writes, and a crash between them loses a turn or leaves an orphan job. Postgres also
gives leases, retries, backoff and reclaim of a dead worker's jobs with no extra system.

Redis carries only live traffic: task events for SSE and stop frames. If Redis is down, SSE falls back to
replaying `task_events`, and without `REDIS_URL` the bus is in memory (single process only).

The cost is latency. `Jobs.Notify()` wakes only a worker in the same process, so a turn starts on the
worker's next poll, up to 1 s after the message. Postgres `LISTEN/NOTIFY` would remove that wait.

## Triggers

Routines have a `trigger` of `time`, `email` or `watch`. All three run in the worker.

| Trigger | Checked | How |
| --- | --- | --- |
| time | schedule ticker | due routines start a task |
| email | every 60 s per mailbox, with per-user jitter | Gmail `history.list` since the last check, matched against the routine's rule |
| watch | per target, `intervalMinutes` (1 to 1440) | each page or search is fetched once, however many routines watch it |

The trigger loop wakes every 5 s (`internal/workers/triggers.go`) and claims due mailboxes and watch targets
with `FOR UPDATE SKIP LOCKED`. A match is written once to `trigger_events`, unique per routine and source.
It then starts a task, or, for a simple watch with no prompt, posts an update directly with no task.

The same loop also runs the contacts sync on its own 30 s ticker (a goroutine, so a slow sync never
delays mail triggers). It claims up to 20 due `contact_syncs` rows with `FOR UPDATE SKIP LOCKED` and a
10 minute lease, and syncs four accounts at a time. Each sync reads the saved contacts and "other contacts"
since the stored People API sync tokens and writes the changes with the new tokens in one transaction,
then sets the next sync 3 hours (plus per-user jitter) later. An expired token (410) triggers a full
resync of that list. A failure backs off as mail does, a 401 sets the account to `needs_auth`, and a row
whose account is gone is deleted. Connecting a contacts account makes its first sync due at once;
removing it deletes its contacts and its sync row.

Limits are 10 email and 20 watch routines per user, and `maxRunsPerHour` per routine (1 to 100, default 10).
A routine's first mailbox check only records a baseline, so mail that arrived before it is not matched.

## Operations

- The env file is shared by both units. `SERVER_BACKGROUND=off` is set only on the server, in
  `/etc/systemd/system/papermark.service.d/background.conf`.
- `deploy.sh` replaces both binaries, keeps `.prev` copies for rollback, restarts both units and checks
  `/health`.
- Any number of servers and workers can run at once. Jobs, schedules, mailboxes, watch targets and contact syncs are all
  claimed with a lease, so a second worker shares the load and a dead one's work is reclaimed.
- The server logs `SERVER_BACKGROUND=off: jobs, schedules and triggers run in papermark-worker` at start.
  The worker logs `job worker: started ... concurrency=16`.
