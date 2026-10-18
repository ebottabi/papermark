# Papermark Agent Server

The Go backend for Keen, a mobile-first agent app for iOS. A user gives Keen a task in plain words. The
server runs an agent loop that calls a model, uses tools (files, shell, web, Gmail and Calendar), and
streams every step back to the phone. Each task runs as a durable job in Postgres, so a restart or a
crashed worker resumes it instead of losing it.

There is no web app. `cmd/server` is the API the iOS app talks to.

- API.md is the HTTP contract with the iOS app.
- docs/ARCHITECTURE.md covers the packages and data layer.
- docs/RUNTIME.md shows what runs in the server and the worker, how a chat turn flows, and how triggers fire.
- docs/HARNESS.md is the design of the agent loop on Fireworks (GLM 5.3 Flash and DeepSeek V4.1 Flash), built in `internal/harness`.
- docs/CLAUDE.md holds the coding rules. The layout follows bee-saas.

## System architecture

```
                         ┌──────────────────────┐
                         │     Keen iOS app     │
                         │  Bearer session token│
                         └───┬──────────────▲───┘
            HTTPS / HTTP     │              │  SSE (task events, live text)
                             ▼              │       APNs push ◄──────────────┐
                    ┌─────────────────────────────┐                          │
                    │  nginx (TLS, proxy, no buf) │                          │
                    └──────────────┬──────────────┘                          │
                                   ▼ 127.0.0.1:8787                          │
┌──────────────────────────────── cmd/server ───────────────────────────────┐│
│                                                                           ││
│  Fiber: recover ─ logger ─ CORS ─ session auth ─ router ─ controllers     ││
│                                                     │                     ││
│                                                     ▼                     ││
│                                        services (one pgx tx per change)   ││
│                                            │                 │            ││
│                          state + enqueue   │                 │ subscribe  ││
│                          (same tx)         │                 │            ││
│  in-process worker (optional) ┐            │                 │            ││
│  schedule ticker ─────────────┤            │                 │            ││
└───────────────────────────────┼────────────┼─────────────────┼────────────┘│
                                │            ▼                 │             │
                ┌───────────────┼─────────────────────┐   ┌────┴──────────┐  │
                │ Postgres      │                     │   │ Redis         │  │
                │  jobs   (SKIP LOCKED queue) ◄──claim┤   │  papermark:   │  │
                │  tasks, task_events, task_messages  │   │  task:<id>    │  │
                │  task_inbox, schedules, devices     │   │  (live bus,   │  │
                │  connectors, oauth_credentials      │   │  stop frames) │  │
                │  projects, knowledge, skills        │   └────▲──────────┘  │
                └───────────────┬─────────────────────┘        │             │
                                ▼ claim                        │ publish     │
┌──────────────────────────────── cmd/worker ───────────────────┴───────────┐│
│                                                                           ││
│  jobs.Worker ── registry ── chat.turn ──► agent loop ──► model API         ││
│   lease, heartbeat,                          │                            ││
│   retry, reclaim                             ├──► workspace files, shell  ││
│                                              ├──► web search / fetch      ││
│                                              ├──► Gmail, Calendar (REST)  ││
│                                              └──► push on finish/ask ─────┼┘
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
                                │
                                ▼
                    WORKSPACE_DIR/<task id>  (shared by server and workers)
```

`cmd/server` runs the HTTP API and, by default, a job worker and the schedule ticker in the same process.
`cmd/worker` runs only the worker and the ticker, so agent runs can scale apart from HTTP. Both can run at
once because jobs are claimed with `FOR UPDATE SKIP LOCKED`.

## Life of a task

```
 iPhone                      cmd/server                 Postgres / Redis            worker
   │ POST /v1/tasks              │                             │                       │
   │────────────────────────────►│ insert task + job (1 tx) ──►│                       │
   │◄──── 201 {id} ──────────────│ Notify()                    │                       │
   │ GET /v1/tasks/:id/events    │                             │◄── claim chat.turn ────│
   │────────────────────────────►│ replay events from Postgres │                       │
   │◄═══ SSE replay ═════════════│ then follow Redis           │                       │
   │                             │                             │      model call       │
   │◄═══ event: delta ═══════════│◄──── publish delta ─────────│◄──────────────────────│
   │                             │                             │      tool calls       │
   │◄═══ AgentEvent ═════════════│◄──── append + publish ──────│◄──────────────────────│
   │                             │                             │                       │
   │        (agent asks a question or wants approval: task parks, job ends)          │
   │◄═══ ask / confirm ══════════│                             │   push to device ──►  │
   │ POST /reply or /confirm     │                             │                       │
   │────────────────────────────►│ save answer + enqueue ─────►│◄── claim, resume ─────│
   │                             │                             │                       │
   │◄═══ idle (result, files) ═══│◄──── append + publish ──────│◄──── done ────────────│
   │ GET /tasks/:id/files/*      │                             │                       │
```

## The agent loop

One claimed `chat.turn` job is one run. The run loops until the task finishes, parks for the user, is
stopped, or the worker shuts down. Every step is a row, so nothing lives only in memory.

```
        load conversation from task_messages
                        │
                        ▼
              ┌───────────────────┐
              │    model call     │──── live text ──► Redis ──► SSE
              │    (streaming)    │
              └─────────┬─────────┘
                        │ store assistant message + token usage (1 tx)
                        ▼
                any tool calls? ── no ──► idle: result + files, task done
                        │ yes
                        ▼
              run each call in order, store each result
              (file_read, file_write, shell_exec, web_search,
               web_fetch, message_notify_user, Gmail and Calendar tools)
                        │
          message_ask_user or an approval? ── yes ──► park, end job
                        │ no                          (/reply or /confirm
                        ▼                              enqueues the next run)
              max steps reached? ── yes ──► stop
                        │ no
                        └──────────► back to the model call

  POST /stop  ─► stop frame on Redis ─► run context cancelled
  worker dies ─► lease expires ─► another worker reclaims the job and
                 continues from the saved conversation
```

The agent's built-in tools are `message_notify_user`, `message_ask_user`, `file_read`, `file_write`,
`shell_exec` (needs the user's approval), `web_search`, `web_fetch` and `idle`. A connected Gmail
account adds its native Gmail tools, a connected Google Calendar account its Calendar tools, a Contacts account the read-only
contacts tools and a Drive account the read-only Drive tools (REST APIs, not MCP).

## Connectors

Keen offers four connectors, Gmail, Calendar, Contacts and Drive. Each is its own Google sign-in with OAuth and narrow scopes, and each can hold several Google accounts.
Contacts and Drive are read-only. Contacts are copied into Postgres by a background sync every 3 hours (the first one shortly after
connecting), so lookups never wait on Google. Drive files are read live, and `drive_download` saves a file into the task workspace,
from where Gmail can attach it.

One-time Google Cloud setup, in the project that holds the Gmail OAuth client: enable the People API and the Google Drive API, add
the scopes `contacts.readonly`, `contacts.other.readonly`, `userinfo.email` and `drive.readonly` to the OAuth consent screen, and add
test users while the app is in testing. `drive.readonly` is a restricted scope and the contacts scopes are sensitive, so a public
app needs Google's verification.

```
 iPhone              cmd/server                    Google
   │ POST /connectors/gmail/authorize                    │
   │──────────────────►│ save state, build auth URL      │
   │◄── {url} ─────────│                                 │
   │ open url in ASWebAuthenticationSession ────────────►│ user consents
   │                   │◄── GET /v1/oauth/callback ──────│
   │                   │ exchange code, store tokens     │
   │◄── redirect back to the app                         │
   │ GET /connectors  ─► status: connected, tools: N     │
```

OAuth needs `PUBLIC_URL` to be an HTTPS domain registered with each provider. Over plain HTTP on an IP
address, the connectors stay in `needs_auth`.

## Background work

Everything slow goes through the `jobs` table. A job with a key (for example a task id) has at most one
active run. Enqueueing an active key is a no-op, or flags the running job to run once more.

```
   enqueue (same tx as the state change) ──► jobs: queued
                                                │ claim (SKIP LOCKED)
                                                ▼
                                             running ── heartbeat extends the lease
                                       ┌────────┼──────────┐
                                    success   error     lease expired
                                       │        │          │ (worker died)
                                       ▼        ▼          ▼
                                     done   retry with   reclaimed, runs again
                                            backoff      (handlers are idempotent)
                                              │
                                     max attempts ──► dead
```

The schedule ticker checks for due schedules on a timer and enqueues a task for each one.

## HTTP API

Accounts are per user. Google sign-in creates the account (or signs in to it) and returns a
session token; a signed-in user may then enroll passkeys and Face ID, which only sign in.
Every other `/v1` route needs `Authorization: Bearer <session token>` and sees only that user's data.
Responses use the envelope `{"success": true, "data": ...}`. API.md has the full shapes.

| Area | Routes |
|------|--------|
| Health | `GET /health`, `GET /ready`, `GET /v1/status` |
| Sign-in | `POST /v1/auth/google`, `POST /v1/auth/passkeys/login/{begin,finish}`, `POST /v1/auth/faceid/{challenge,login}` |
| Account | `GET /v1/me`, `GET /v1/auth/methods`, `POST /v1/auth/logout`, `POST /v1/auth/passkeys/register/{begin,finish}`, `GET /v1/auth/passkeys`, `DELETE /v1/auth/passkeys/:id`, `POST /v1/auth/faceid/register`, `GET /v1/auth/faceid`, `DELETE /v1/auth/faceid/:id` |
| Tasks | `GET/POST /v1/tasks`, `GET/PATCH /v1/tasks/:id`, `POST /v1/tasks/:id/{reply,confirm,messages,stop,fork}` |
| Live events | `GET /v1/tasks/:id/events` (SSE, resumes after `Last-Event-ID`) |
| Task output | `GET /v1/tasks/:id/files`, `GET /v1/tasks/:id/files/*`, `GET /v1/tasks/:id/transcript` |
| Usage | `GET /v1/usage` |
| Schedules | `GET/POST /v1/schedules`, `PATCH/DELETE /v1/schedules/:id`, `POST /v1/schedules/:id/run` |
| Connectors | `GET/POST /v1/connectors`, `DELETE /v1/connectors/:name`, `POST /v1/connectors/:name/authorize`, `GET /v1/oauth/callback` |
| Devices | `POST /v1/devices`, `DELETE /v1/devices/:token` |
| Knowledge, skills | `GET/POST /v1/{knowledge,skills}`, `PATCH/DELETE /v1/{knowledge,skills}/:id` |

## Code layout

```
cmd/
  server/        HTTP API + in-process worker + schedule ticker; migrates on boot
  worker/        worker + schedule ticker only
internal/
  router/        Fiber app, /health, /ready, every /v1 route
  middleware/    session auth, request log, rate limit
  controllers/   parse and validate, call a service, send the envelope
  handlers/      shared envelope helpers (SendSuccess, ...)
  responses/     response DTOs, field names exactly as in API.md
  services/      business logic and transactions; auth, passkeys, Face ID
    jobs/        durable queue: enqueue, claim, lease, retry, reclaim
  harness/       agent loop: ChatTurn, turn, tools, web, task event log
  workers/       job handlers (chat.turn) and the schedule ticker
  models/        schema structs (GORM tags drive migration only)
    queries/     raw SQL per model over pgx
  integrations/  model client, MCP client
  notifications/ APNs and ntfy push
  cache/         Redis cache-aside and the live event bus
  storage/       local disk and per-task workspaces
  database/      pgx pool, migrator, Redis client, test databases
  config/        environment loading
deploy/          systemd units and env example
docs/            architecture, harness proposal, coding rules
```

## Run locally

Postgres must be running. Redis is optional for `cmd/server`; without it, live events stay in process.
`cmd/worker` needs Redis to reach the server's subscribers.

```sh
cp .env.example .env   # then set GOOGLE_CLIENT_IDS, DATABASE_URL and the model key
make run               # builds bin/server and starts it on 127.0.0.1:8787
make run-worker        # optional: a standalone worker
```

```sh
# exchange a Google ID token (from the app's Google sign-in) for a session token
curl -H 'Content-Type: application/json' -d '{"idToken":"<google id token>"}' \
  http://127.0.0.1:8787/v1/auth/google
curl -H 'Authorization: Bearer <token>' http://127.0.0.1:8787/v1/tasks
curl -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"task":"Plan a weekend in Lisbon"}' http://127.0.0.1:8787/v1/tasks
```

## Configuration

| Variable | Purpose |
|----------|---------|
| `HOST`, `PORT` | Listen address. Default `127.0.0.1:8787` behind nginx. |
| `PUBLIC_URL` | Public base URL, used for OAuth redirect URIs and as the passkey defaults. |
| `GOOGLE_CLIENT_IDS` | Comma list of Google OAuth client ids whose ID tokens sign in (the iOS client id). Empty means Google sign-in fails. |
| `WEBAUTHN_RP_ID`, `WEBAUTHN_RP_NAME`, `WEBAUTHN_ORIGINS` | Passkey relying party: the domain (default the `PUBLIC_URL` host), display name (default `Keen`), allowed origins (comma list, default `PUBLIC_URL`). |
| `SESSION_TTL_HOURS` | Session token lifetime. Default 720 (30 days). |
| `ENVIRONMENT` | `development` or `production`. |
| `DATABASE_URL` or `DB_*` | Postgres. `DATABASE_URL` wins. |
| `REDIS_URL` or `REDIS_*` | Redis live bus. Optional for the server, required for a standalone worker. |
| `FIREWORKS_API_KEY`, `FIREWORKS_BASE_URL` | Model provider. Tasks fail with a clear error while the key is empty. |
| `AGENT_QUALITY_MODEL`, `AGENT_SPEED_MODEL` | GLM 5.3 Flash runs every task; DeepSeek V4.1 Flash is its fallback. Mode sets the reasoning effort (`speed` low, `quality` medium). |
| `AGENT_CONTEXT_BUDGET`, `AGENT_MODEL_TIMEOUT_SECONDS`, `AGENT_MODEL_MAX_RETRIES` | Trim threshold, per-call timeout, retries before anything streams. |
| `TAVILY_API_KEY` | Enables `web_search`. |
| `AGENT_MAX_TOKENS`, `AGENT_MAX_STEPS` | Per-call output cap and per-run step cap. |
| `ROUTINE_MAX_STEPS` | Step cap for runs started by a routine (default 40, never above `AGENT_MAX_STEPS`). |
| `WORKSPACE_DIR` | Per-task files. Server and workers must share it. |
| `WORKER_CONCURRENCY`, `WORKER_LEASE_SECONDS`, `WORKER_POLL_MS` | Job worker tuning. |
| `GMAIL_CLIENT_ID`, `GMAIL_CLIENT_SECRET`, `GMAIL_SCOPES`, `GMAIL_API_URL` | Gmail connector OAuth and API origin. |
| `CALENDAR_CLIENT_ID`, `CALENDAR_CLIENT_SECRET`, `CALENDAR_SCOPES`, `CALENDAR_API_URL` | Calendar connector OAuth (client defaults to the Gmail one) and API origin. |
| `CONTACTS_CLIENT_ID`, `CONTACTS_CLIENT_SECRET`, `CONTACTS_SCOPES`, `CONTACTS_API_URL` | Contacts connector OAuth (client defaults to the Gmail one) and People API origin. |
| `DRIVE_CLIENT_ID`, `DRIVE_CLIENT_SECRET`, `DRIVE_SCOPES`, `DRIVE_API_URL` | Drive connector OAuth (client defaults to the Gmail one, scope `drive.readonly`) and API origin. |
| `TOKEN_ENCRYPTION_KEY` | 32 bytes, base64. Encrypts stored OAuth tokens at rest. |
| `APNS_KEY_PATH`, `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_TOPIC` | Push. Off unless all four are set. |
| `NTFY_URL`, `NTFY_TOPIC`, `NTFY_TOPIC_SECRET`, `NTFY_TOKEN` | Push through ntfy. Off unless the URL, the topic prefix and a secret (`NTFY_TOPIC_SECRET`, else `TOKEN_ENCRYPTION_KEY`) are set. Each user's topic is `<NTFY_TOPIC>-<12 hex>` and appears as `ntfyTopic` in `GET /v1/me/preferences`. `NTFY_TOKEN` is an optional bearer token. |
| `BODY_LIMIT_BYTES` | Request body cap. |

## Deploy

The server runs on one Ubuntu host as the `papermark` systemd service, beside other sites.

```
 internet ──► :80/:443 nginx ──► 127.0.0.1:8787 papermark.service (cmd/server)
                                       │
                                       ├── Postgres  127.0.0.1:5432  db papermark
                                       └── /var/lib/papermark/workspaces

 /opt/papermark/bin/server         binary (linux/amd64, CGO_ENABLED=0)
 /etc/papermark/papermark.env      environment (640 root:papermark)
 /etc/systemd/system/papermark.service
 /etc/nginx/sites-available/keen-api
```

Build on your machine and copy the binary up. The host does not need Go.

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/server-linux ./cmd/server
scp bin/server-linux root@<host>:/root/server
ssh root@<host> 'install -m 755 /root/server /opt/papermark/bin/server && systemctl restart papermark'
curl http://<host>/health
```

`deploy/papermark-worker.service` runs a standalone worker when agent runs need their own machines.
With the worker installed, keep the server to the API. Jobs, schedules and trigger pollers then run
only in the worker, and new tasks start within `WORKER_POLL_MS`. The env file is shared, so set the switch on the server unit:

```sh
ssh root@<host> 'mkdir -p /etc/systemd/system/papermark.service.d && printf "[Service]\nEnvironment=SERVER_BACKGROUND=off\n" > /etc/systemd/system/papermark.service.d/background.conf && systemctl daemon-reload && systemctl restart papermark'
```

### CI/CD

`.github/workflows/deploy.yml` runs on push to `master` and on manual dispatch: vet, build and
`go test -race` against Postgres and Redis service containers, then cross-compiles `cmd/server` and
`cmd/worker` (linux/amd64), copies them to the host over SSH and runs `deploy/deploy.sh`. The script
installs each binary atomically, keeps the old one as `<name>.prev`, restarts the services, waits for
`/health`, and restores the `.prev` binaries if the check fails (the job then fails). Deploys never overlap.

The server still runs the in-process job worker, and the standalone worker runs beside it. That is safe:
jobs and schedules are claimed with `FOR UPDATE SKIP LOCKED` and a lease. `deploy.sh` restarts the worker
only once `papermark-worker.service` is installed.

One-time setup:

```sh
# 1. Dedicated deploy key (no passphrase)
ssh-keygen -t ed25519 -N '' -C papermark-deploy -f ./papermark_deploy

# 2. Authorize it on the host (use a restricted user instead of root if you prefer;
#    it must be able to write /opt/papermark/bin and run systemctl)
ssh-copy-id -i ./papermark_deploy.pub root@<host>

# 3. Install the worker unit (needs REDIS_* in /etc/papermark/papermark.env)
scp deploy/papermark-worker.service root@<host>:/etc/systemd/system/
ssh root@<host> 'systemctl daemon-reload && systemctl enable papermark-worker'

# 4. GitHub secrets
gh secret set DEPLOY_HOST --body '<host>'
gh secret set DEPLOY_USER --body 'root'
gh secret set DEPLOY_SSH_KEY < ./papermark_deploy
ssh-keyscan -t ed25519 <host> | gh secret set DEPLOY_KNOWN_HOSTS   # verify the fingerprint first

# 5. Delete the local private key
rm ./papermark_deploy
```

## Test

```sh
make test-race
```

The tests create throwaway databases on `TEST_DATABASE_URL` (default: the local server as the current OS
user) and use `TEST_REDIS_URL` (default `redis://localhost:6379/0`). The HTTP tests in internal/router
build cmd/server and run it as a real process, killing and restarting it to check recovery.
