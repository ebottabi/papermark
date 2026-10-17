# Agent server API

The contract between the agent server (this repo) and the Keen iOS client.
Change it only together with both sides.

The server is Go and follows the bee-saas layout and conventions (see docs/ARCHITECTURE.md).

All paths below are under `/v1` (for example `GET /v1/tasks`), except `GET /health` and `GET /ready`.
Every request carries `Authorization: Bearer <session token>` (see Auth), except `/health`, `/ready`, the sign-in endpoints and `GET /v1/oauth/callback`, which a browser opens.

JSON responses use the bee-saas envelope. The "Result" column below describes `data`:

```
success: {"success": true, "data": <result>, "message"?: "..."}
error:   {"success": false, "error": "<message>", "code": "BAD_REQUEST" | "UNAUTHORIZED" | "FORBIDDEN" | "NOT_FOUND" | "CONFLICT" | "PAYLOAD_TOO_LARGE" | "INTERNAL_ERROR"}
```

Raw responses are not enveloped: SSE streams, file downloads, the markdown transcript, and the OAuth callback redirect.
JSON field names stay exactly as written in this document. Times are RFC 3339 strings with milliseconds, in UTC.

## Auth

Accounts are per user and created only by Google sign-in. There is no Sign in with Apple and no password.
The three ways to sign in:

1. **Google** (always available). Creates the account on first use and signs in after that.
2. **Passkey**. Enrolled by a signed-in user; signs in to that account and never creates one.
3. **Face ID**. A per-device Secure Enclave P-256 key enrolled by a signed-in user; signs in to that account and never creates one.

Every sign-in returns the same `Session`, whose `token` is an opaque bearer token. The server stores only its SHA-256.
A missing, unknown, expired or revoked token answers 401 `UNAUTHORIZED`.

```ts
type User = { id: string; email: string; name: string; picture: string; createdAt: string }
type Session = { token: string; expiresAt: string; user: User; created: boolean }   // created: Google made the account just now
type Credential = { id: string; name: string; createdAt: string; lastUsedAt: string | null }
```

Data is per user. Every task, project, knowledge entry, skill, schedule, connector, OAuth token and device belongs to the user
who created it. Another user's resource answers 404, never 403, and never appears in a list.
Push goes only to the task owner's devices, scheduled runs and forks belong to the schedule's or source task's owner, and a
connector's OAuth flow is bound to the user who started it.

Sign-in endpoints (no bearer token; rate limited to 20 requests a minute per IP):

| Method | Path | Body | Result |
|---|---|---|---|
| POST | /auth/google | `{idToken}` | `Session`. 401 for a bad signature, issuer, audience or expiry, or an unverified email; 409 when another account already uses the email |
| POST | /auth/passkeys/login/begin | | `{ceremonyId, options}`; `options` is WebAuthn `PublicKeyCredentialRequestOptions` (`{publicKey: {...}}`), discoverable (no `allowCredentials`), `userVerification: "required"` |
| POST | /auth/passkeys/login/finish | `{ceremonyId, credential}` | `Session` (`created: false`). `credential` is the assertion as JSON (base64url fields) |
| POST | /auth/faceid/challenge | `{keyId}` | `{challengeId, challenge, expiresAt}`; `challenge` is 32 random bytes, standard base64, single use, valid 2 minutes. 404 for an unknown or revoked key |
| POST | /auth/faceid/login | `{keyId, challengeId, signature}` | `Session` (`created: false`). `signature` is the standard base64 DER ECDSA signature of SHA-256 over the decoded challenge bytes (`.ecdsaSignatureMessageX962SHA256`). Any failure is 401 "Face ID sign-in failed" |

Account endpoints (bearer token):

| Method | Path | Body | Result |
|---|---|---|---|
| GET | /me | | `User` |
| GET | /auth/methods | | `{passkey: boolean, faceId: boolean}`: whether the user has enrolled each |
| POST | /auth/logout | | `{ok: true}`; revokes this token |
| POST | /auth/passkeys/register/begin | | `{ceremonyId, options}`; WebAuthn `PublicKeyCredentialCreationOptions` with `residentKey: "required"`, `userVerification: "required"` |
| POST | /auth/passkeys/register/finish | `{ceremonyId, name, credential}` | 201 `Credential`. `credential` is the attestation as JSON |
| GET | /auth/passkeys | | `Credential[]` |
| DELETE | /auth/passkeys/:id | | `{ok: true}` |
| POST | /auth/faceid/register | `{deviceName, publicKey}` | 201 `Credential` (its `id` is the `keyId`). `publicKey` is standard base64 of the X9.63 uncompressed point (`SecKeyCopyExternalRepresentation`) or SPKI DER |
| GET | /auth/faceid | | `Credential[]` |
| DELETE | /auth/faceid/:id | | `{ok: true}`; the key can no longer sign in |

Register endpoints are rate limited to 30 requests a minute per IP. A WebAuthn ceremony's state is kept on the server for
5 minutes under `ceremonyId` and used once. Passkeys need `WEBAUTHN_RP_ID` to be an HTTPS domain that serves
`apple-app-site-association` with `webcredentials` for the app.

## Storage

Postgres holds users, sessions, passkeys, Face ID keys, sign-in challenges, tasks, events, model messages, run jobs, devices, schedules, projects, knowledge, skills, connectors and OAuth tokens.
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
  | { type: "confirm"; name: string; input: unknown; allowAlways: boolean }      // agent waits for /confirm
  | { type: "reply"; text: string }                          // the user's answer to an ask
  | { type: "decision"; name: string; decision: Decision }   // the user's answer to a confirm
  | { type: "message"; text: string; attachments: string[] } // a user message: the first prompt, a queued message, or a continuation
  | { type: "idle"; text: string; files: string[] }          // done; files are workspace-relative deliverables
  | { type: "stopped"; text: string }
  | { type: "error"; text: string }
  | { type: "update"; id: string; importance: Importance; title: string; body: string; sections: Section[] } // a published update; the task keeps running
type Importance = "quiet" | "feed" | "push"
type Section = { title: string; items: { text: string; detail?: string; url?: string }[] }
type Update = {
  id: string; taskId: string; scheduleId: string | null; importance: Importance
  title: string; body: string; sections: Section[]; read: boolean; createdAt: string
}
type Memory = { id: string; text: string; sourceTaskId: string | null; createdAt: string; updatedAt: string }
type Preferences = { timezone: string; quietStart: string; quietEnd: string; pushLimit: number; newsTopics: string[]; ntfyTopic: string | null }
type Decision = "allow" | "always" | "deny"
```

The first event of every task is its `message` (the prompt). Clients must ignore event types they do not know.

`tool` names: built-ins `message_notify_user`, `file_read`, `file_write`, `shell_exec`, `web_search`, `web_fetch`,
`publish_update`, `memory_save`, `memory_search`, `routine_create`;
connector tools are `<connector>_<tool>`. `web_search` is offered only when `TAVILY_API_KEY` is set. Both web tools
are ordinary tools: each emits a `tool` event with the query/URL as input and the result text as output.

Approval (`confirm`) is required before: every `shell_exec`, every `routine_create` (`allowAlways: true`), and every connector tool not annotated `readOnlyHint`.
`always` applies to that tool name for the rest of the task, except where the confirm event has `allowAlways: false`
(`gmail_send`, `gmail_send_draft`, `gmail_forward`): `POST /tasks/:id/confirm` with `always` then returns 400
"this action needs approval every time" and the task keeps waiting.

## Live events: `GET /tasks/:id/events`

Server-sent events. Persisted events are sent as `id: <seq>\ndata: <AgentEvent JSON>\n\n`; resume with the
`Last-Event-ID` header (or `?from=<seq>`) and the server replays everything after it.

Streaming text is sent as frames **without an `id:` line**, with `event: delta`:
`event: delta\ndata: {"text": "<chunk>"}\n\n`. Deltas are not persisted and not replayed; they preview the text
of the next `notify`/`ask`/`idle` and are superseded by it. A client shows them as a live typing line and clears it
when the next persisted event arrives.

What the model is doing before its text arrives is sent the same way, with `event: activity`:
`event: activity\ndata: {"name": "thinking"}\n\n` once per model call when it starts reasoning, and
`{"name": "<tool>"}` once per tool call when the model starts writing that tool's arguments (for example
`file_write`). A client shows it as a status line and clears it on the next delta or persisted event.

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
(idempotent by token; registering a token another user had moves it to the signed-in user). `DELETE /devices/:token`.
The server sends an APNs alert on `ask`, `confirm`, `idle`, `error`, and on an `update` event whose importance is `push` (alert text = the update title) with payload `{"taskId": "<id>", "type": "<event type>"}`,
when configured with env `APNS_KEY_PATH` (.p8), `APNS_KEY_ID`, `APNS_TEAM_ID`, `APNS_TOPIC` (the app bundle id).
The same events are also POSTed to the owner's ntfy topic when `NTFY_URL`, `NTFY_TOPIC` and a secret are set: `Title` is the routine title or "Keen", the body is the alert text, `Click` is `keen://task/<id>`, `Tags` maps the event (`ask` question, `confirm` warning, `idle` white_check_mark, `error` x, `update` bell) and `Priority` is high for `ask` and `confirm`. APNs and ntfy fail independently.
Without them push is off and `POST /devices` still stores the token.

## Schedules (routines)

`ScheduledTask = {id, title, kind: "custom" | "brief", prompt, mode, projectId, repeat: "hourly" | "daily" | "weekdays" | "weekly", time: "HH:MM", weekday: 1-7 | null, enabled, lastRunAt, nextRunAt, createdAt}`
`time` and `weekday` are in the owner's `Preferences.timezone` (DST-aware; `weekdays` is Monday to Friday; hourly uses the minute of `time`).
Changing the time zone moves every enabled schedule's `nextRunAt` so it keeps its wall-clock time.
GET /schedules → `{schedules: ScheduledTask[]}`, POST /schedules, PATCH /schedules/:id, DELETE /schedules/:id, POST /schedules/:id/run (run now → `{id}` of the task).
A run creates a normal task with `scheduleId` set. `title` is optional on create (default: the first 40 characters of the prompt, whitespace collapsed; at most 120 characters).
`POST /schedules {kind: "brief", time, repeat}` without `prompt` and `title` gets the title "Morning brief" and the server's brief prompt: Email (`gmail_digest` with `sinceLast`, only when Gmail is connected; item `detail` = sender), News (web search and fetch on `Preferences.newsTopics`, else general headlines; item `url` = article link) and Weather (for the time zone's main city, when reachable), published as one `feed` update with empty sections omitted.
A routine run has a lower step cap (`ROUTINE_MAX_STEPS`, default 40) and is told to end with exactly one `publish_update`. After three runs in a row end in `error` the routine is disabled (`enabled: false`, `nextRunAt: null`) and a `feed` update titled "<title> turned off" is added. A successful run resets the count; editing the routine resets it too.

## Preferences

`GET /me/preferences` and `PATCH /me/preferences` (partial body, same shape returned) → `Preferences`.
Defaults: `timezone "UTC"` (IANA name), `quietStart "22:00"`, `quietEnd "07:00"` (HH:MM, may wrap midnight), `pushLimit 5` (0 to 20 pushes per local day), `newsTopics []` (at most 20, 60 characters each).
`ntfyTopic` is read-only: the user'"'"'s private ntfy topic (`<NTFY_TOPIC>-<12 hex>`, derived from the user id and a server secret), or `null` when ntfy is not configured. The app shows "Subscribe in ntfy" for it on `NTFY_URL`.
Invalid values return 400.

## Updates

`GET /updates?before=<RFC3339>&limit=30` → `{updates: Update[]}`, newest first, `quiet` updates excluded; `before` is exclusive (pass the last `createdAt` to get the next page); `limit` is 1 to 100.
`POST /updates/:id/read` and `POST /updates/read-all` → 204. Another user's update is 404.
The agent tool `publish_update {importance, title, body, sections?}` writes an update and emits the `update` event (non-terminal). A `push` is stored and delivered as `feed` when the owner is in quiet hours or has already received `pushLimit` pushes today (owner's time zone); the event and the row carry the importance actually used.

## Memories

`GET /memories` → `{memories: Memory[]}` (newest first), `POST /memories {text}` → 201 `Memory` (200 with the existing memory when near-identical text, ignoring case, spacing and punctuation, is already saved), `PATCH /memories/:id {text}` (409 when it would duplicate another), `DELETE /memories/:id` → 204. `text` is at most 1000 characters and needs a letter or digit.
The agent saves with `memory_save {text}` (no approval) and looks up with `memory_search {query}` (top 10). Up to 15 memories, best full-text matches to the prompt first and then the most recent, go into each new task's first message after the knowledge.

## Projects, knowledge, skills

- `Project = {id, name, instructions, createdAt}`. GET/POST /projects, PATCH/DELETE /projects/:id.
  A task in a project gets the project's instructions in its first message. Deleting a project leaves its tasks with `projectId: null`.
- `Knowledge = {id, title, content, enabled, createdAt}`. GET/POST /knowledge, PATCH/DELETE /knowledge/:id.
  Enabled entries go into every new task's first message (not the system prompt, which stays byte-stable for caching).
- `Skill = {id, name, description, instructions, createdAt}` (`name` matches `^[a-z0-9-]{1,40}$`, unique). GET/POST /skills, PATCH/DELETE /skills/:id.

## Connectors

`Connector = {name, kind: "mcp" | "gmail", url, status: "connected" | "needs_auth" | "error", error: string | null, tools: number, account: string | null}`.
`account` is the signed-in address of a `gmail` connector (null for `mcp`). A Gmail call Google rejects with 401, or a refresh
that fails, sets the connector to `needs_auth`; the tool result then reads "Gmail needs reconnecting in Connectors".
A failed authorization on an `mcp` connector does the same.

| Method | Path | Body | Result |
|---|---|---|---|
| GET | /connectors | | `{connectors: Connector[]}`, including catalog connectors not added yet (`needs_auth`, or `error` "Not set up on this server" without a URL) |
| POST | /connectors | `{name, url}` | `Connector` (tries to connect; `needs_auth` if the server demands OAuth) |
| DELETE | /connectors/:name | | removes it and its tokens |
| POST | /connectors/:name/authorize | | `{authorizationUrl}` to open in a browser; adds a catalog connector on first use |
| GET | /v1/oauth/callback?code&state | | no bearer auth; validates `state`, stores tokens, reconnects, then redirects to `keen://oauth?connector=<name>&status=connected\|error` |

The catalog is `gmail` (kind `gmail`), signed in with Google OAuth and served by the Gmail REST API directly (env `GMAIL_API_URL`,
default `https://gmail.googleapis.com`). The app offers only this one. It needs `GMAIL_CLIENT_ID` and `GMAIL_CLIENT_SECRET`;
`GMAIL_SCOPES` defaults to `gmail.modify gmail.compose`. The authorization URL adds `access_type=offline&prompt=consent` so Google
returns a refresh token, and carries no `resource` parameter. After sign-in the server reads the mailbox profile (`account`).
Deleting the connector revokes the token at Google first, best effort.

Gmail tools (all inputs camelCase). `*` needs approval, and `confirm` with `always` on a `!` tool returns 400
"this action needs approval every time" (the confirm event carries `allowAlways: false`).

| Tool | Input |
|---|---|
| `gmail_search` | `{query?, maxResults? (20, max 100), pageToken?}` |
| `gmail_get_message` | `{id}` |
| `gmail_get_thread` | `{threadId}` |
| `gmail_digest` | `{query?, sinceLast?, max? (50)}` |
| `gmail_list_labels` | `{}` |
| `gmail_get_attachment` | `{messageId, attachmentId, filename?}` saves into the task workspace and returns the path |
| `gmail_create_draft` | `{to[], cc[], bcc[], subject, body, replyToMessageId?, replyAll?, attachments? (workspace paths)}` |
| `gmail_send` *! | same as `gmail_create_draft` |
| `gmail_send_draft` *! | `{draftId}` |
| `gmail_forward` *! | `{messageId, to[], cc[], note?}` with the original attachments |
| `gmail_modify` | `{ids[], addLabels[], removeLabels[], markRead?, archive?, star?}` |
| `gmail_trash` * | `{ids[]}` |

Other connectors (`kind: "mcp"`) use the MCP SDK's client auth (dynamic client registration, PKCE); `<NAME>_CLIENT_ID`,
`_CLIENT_SECRET` and `_SCOPES` override registration and the advertised scopes.

OAuth tokens are encrypted at rest with AES-256-GCM when env `TOKEN_ENCRYPTION_KEY` (32 bytes, base64) is set; rows written
before it was set stay readable and are sealed on their next save.

The redirect URI is `${PUBLIC_URL}/v1/oauth/callback`
(env `PUBLIC_URL`, default `http://127.0.0.1:${PORT}`). New tasks use whatever connectors are connected when they start.

## Without an API key

With `FIREWORKS_API_KEY` empty every task ends in an `error` event naming the missing key. There is no model stand-in;
the test suite never calls Fireworks.
