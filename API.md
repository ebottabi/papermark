# Agent server API

The contract between the agent server (this repo) and the Keen iOS client.
Change it only together with both sides.

The server is Go and follows the bee-saas layout and conventions (see docs/ARCHITECTURE.md).

All paths below are under `/v1` (for example `GET /v1/tasks`), except `GET /health` and `GET /ready`.
Every request carries `Authorization: Bearer <API_TOKEN>`, except `/health`, `/ready` and `GET /v1/oauth/callback`, which a browser opens.

JSON responses use the bee-saas envelope. The "Result" column below describes `data`:

```
success: {"success": true, "data": <result>, "message"?: "..."}
error:   {"success": false, "error": "<message>", "code": "BAD_REQUEST" | "UNAUTHORIZED" | "FORBIDDEN" | "NOT_FOUND" | "CONFLICT" | "PAYLOAD_TOO_LARGE" | "INTERNAL_ERROR"}
```

Raw responses are not enveloped: SSE streams, file downloads, the markdown transcript, and the OAuth callback redirect.
JSON field names stay exactly as written in this document. Times are RFC 3339 strings with milliseconds, in UTC.

## Storage

Postgres holds tasks, events, model messages, run jobs, devices, schedules, projects, knowledge, skills, connectors and OAuth tokens.
Redis carries live events (deltas included) between the worker and the HTTP server.
Each task's files live on disk in `WORKSPACE_DIR/<task id>/` (default `data/workspaces`).

Runs are durable jobs. A task that is waiting on an ask or a confirm holds no process. The reply, the decision or a new message enqueues the next run.
If a worker dies mid-run, the job is retried from the saved conversation. A tool call that started but never recorded a result gets a "Not run: the task was interrupted." result.

## Types

```ts
type Status = "running" | "waiting" | "done" | "stopped" | "error"
type Mode = "speed" | "quality"          // speed = effort "low", quality = effort "high"
type Usage = { inputTokens: number; outputTokens: number; cacheReadTokens: number; cacheWriteTokens: number }

type TaskSummary = {
  id: string; prompt: string; createdAt: string; updatedAt: string; status: Status
  mode: Mode; projectId: string | null; scheduleId: string | null
  favorite: boolean; archived: boolean; usage: Usage
}

type AgentEvent =
  | { type: "notify"; text: string }                         // assistant message
  | { type: "ask"; text: string }                            // agent waits for /reply
  | { type: "tool"; name: string; input: unknown; output: string; isError: boolean }
  | { type: "confirm"; name: string; input: unknown }        // agent waits for /confirm
  | { type: "reply"; text: string }                          // the user's answer to an ask
  | { type: "decision"; name: string; decision: Decision }   // the user's answer to a confirm
  | { type: "message"; text: string; attachments: string[] } // a user message: the first prompt, a queued message, or a continuation
  | { type: "idle"; text: string; files: string[] }          // done; files are workspace-relative deliverables
  | { type: "stopped"; text: string }
  | { type: "error"; text: string }
type Decision = "allow" | "always" | "deny"
```

The first event of every task is its `message` (the prompt). Clients must ignore event types they do not know.

`tool` names: built-ins `message_notify_user`, `file_read`, `file_write`, `shell_exec`, `web_search`, `web_fetch`;
connector tools are `<connector>_<tool>`. `web_search`/`web_fetch` are Anthropic server tools; the server emits a
`tool` event for each one with the query/URL as input and a short text summary of the result as output.

Approval (`confirm`) is required before: every `shell_exec`, and every connector tool not annotated `readOnlyHint`.
`always` applies to that tool name for the rest of the task.

## Live events: `GET /tasks/:id/events`

Server-sent events. Persisted events are sent as `id: <seq>\ndata: <AgentEvent JSON>\n\n`; resume with the
`Last-Event-ID` header (or `?from=<seq>`) and the server replays everything after it.

Streaming text is sent as frames **without an `id:` line**, with `event: delta`:
`event: delta\ndata: {"text": "<chunk>"}\n\n`. Deltas are not persisted and not replayed; they preview the text
of the next `notify`/`ask`/`idle` and are superseded by it. A client shows them as a live typing line and clears it
when the next persisted event arrives.

A comment line `: ping` is sent every 20 s so idle connections stay open.

## Tasks

| Method | Path | Body | Result |
|---|---|---|---|
| GET | /tasks?archived=true\|false (default false)&projectId= | | `{tasks: TaskSummary[]}` newest `updatedAt` first |
| POST | /tasks | `{task, mode?, projectId?, attachments?: [{name, base64}]}` | 202 `{id}` |
| GET | /tasks/:id | | `TaskSummary & {events: AgentEvent[]}` |
| PATCH | /tasks/:id | `{favorite?, archived?}` | `TaskSummary` |
| POST | /tasks/:id/reply | `{text}` | 409 unless waiting on an ask |
| POST | /tasks/:id/confirm | `{decision}` | 409 unless waiting on a confirm |
| POST | /tasks/:id/messages | `{text, attachments?}` | running: queued, delivered with the next tool results. done/stopped/error: the task continues with its history (status back to running). waiting: 409, use reply/confirm |
| POST | /tasks/:id/stop | | 409 unless running or waiting |
| POST | /tasks/:id/fork | `{task}` | 202 `{id}`: a new task whose history is a copy of this one's, plus the new message; workspace copied |
| GET | /tasks/:id/files | | `{files: [{path, size, modifiedAt}]}` |
| GET | /tasks/:id/files/<path> | | raw bytes, Content-Type by extension; path-escape refused |
| GET | /tasks/:id/transcript | | text/markdown transcript for sharing |

Attachments are saved into the workspace under `uploads/`; total body limit 20 MB. The message tells the agent the paths.

A message starting with `/<skill-name>` expands that skill's instructions into the message on the server.

## Usage

`GET /usage?days=30` → `{days: [{date: "YYYY-MM-DD", usage: Usage, tasks: number}], total: Usage}`.

## Push

`POST /devices {token, platform: "ios", environment: "sandbox" | "production"}` registers an APNs device token
(idempotent by token). `DELETE /devices/:token`.
The server sends an APNs alert on `ask`, `confirm`, `idle`, `error` with payload `{"taskId": "<id>", "type": "<event type>"}`,
when configured with env `APNS_KEY_PATH` (.p8), `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_TOPIC` (the app bundle id).
Without them push is off and `POST /devices` still stores the token.

## Schedules

`ScheduledTask = {id, prompt, mode, projectId, repeat: "hourly" | "daily" | "weekly", time: "HH:MM", weekday: 1-7 | null, enabled, lastRunAt, nextRunAt, createdAt}`
(time/weekday in the server's local time zone; hourly uses the minute of `time`.)
GET /schedules → `{schedules: ScheduledTask[]}`, POST /schedules, PATCH /schedules/:id, DELETE /schedules/:id, POST /schedules/:id/run (run now → `{id}` of the task).
A run creates a normal task with `scheduleId` set.

## Projects, knowledge, skills

- `Project = {id, name, instructions, createdAt}`. GET/POST /projects, PATCH/DELETE /projects/:id.
  A task in a project gets the project's instructions in its first message. Deleting a project leaves its tasks with `projectId: null`.
- `Knowledge = {id, title, content, enabled, createdAt}`. GET/POST /knowledge, PATCH/DELETE /knowledge/:id.
  Enabled entries go into every new task's first message (not the system prompt, which stays byte-stable for caching).
- `Skill = {id, name, description, instructions, createdAt}` (`name` matches `^[a-z0-9-]{1,40}$`, unique). GET/POST /skills, PATCH/DELETE /skills/:id.

## Connectors

`Connector = {name, url, status: "connected" | "needs_auth" | "error", error: string | null, tools: number}`.

| Method | Path | Body | Result |
|---|---|---|---|
| GET | /connectors | | `{connectors: Connector[]}`, including catalog connectors not added yet (`needs_auth`, or `error` "Not set up on this server" without a URL) |
| POST | /connectors | `{name, url}` | `Connector` (tries to connect; `needs_auth` if the server demands OAuth) |
| DELETE | /connectors/:name | | removes it and its tokens |
| POST | /connectors/:name/authorize | | `{authorizationUrl}` to open in a browser; adds a catalog connector on first use |
| GET | /v1/oauth/callback?code&state | | no bearer auth; validates `state`, stores tokens, reconnects, then redirects to `keen://oauth?connector=<name>&status=connected\|error` |

The catalog is `gmail` and `github`, both OAuth: env `GMAIL_MCP_URL` (default `https://gmailmcp.googleapis.com/mcp/v1`) and
`GITHUB_MCP_URL` (default `https://api.githubcopilot.com/mcp/`). The app offers only these.
A catalog connector with `<NAME>_CLIENT_ID` (and `_CLIENT_SECRET`) uses that OAuth client instead of dynamic registration;
neither Google nor GitHub supports registration, so both need one. `<NAME>_SCOPES` replace the scopes the server advertises
(defaults: Gmail `gmail.readonly gmail.compose`, GitHub `repo read:org read:user user:email`). Gmail's authorization URL adds
`access_type=offline&prompt=consent` so Google returns a refresh token.

OAuth uses the MCP SDK's client auth (dynamic client registration, PKCE). The redirect URI is `${PUBLIC_URL}/v1/oauth/callback`
(env `PUBLIC_URL`, default `http://127.0.0.1:${PORT}`). New tasks use whatever connectors are connected when they start.

## Testing without an API key

`ANTHROPIC_BASE_URL` can point at `cmd/fakeanthropic` (`go run ./cmd/fakeanthropic -port 8790 -script testdata/lisbon.json`),
a scripted stand-in for the Messages API (streaming included), so the whole stack, iOS UI tests included, runs end to end
without a key. Its package doc explains the script format.
