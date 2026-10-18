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
  id: string; taskId: string | null; scheduleId: string | null; importance: Importance
  title: string; body: string; sections: Section[]; read: boolean; createdAt: string
}
type Memory = { id: string; text: string; sourceTaskId: string | null; createdAt: string; updatedAt: string }
type Preferences = { timezone: string; quietStart: string; quietEnd: string; pushLimit: number; newsTopics: string[]; ntfyTopic: string | null }
type Decision = "allow" | "always" | "deny"
```

The first event of every task is its `message` (the prompt). Clients must ignore event types they do not know.

`tool` names: built-ins `message_notify_user`, `file_read`, `file_write`, `shell_exec`, `web_search`, `web_fetch`,
`publish_update`, `memory_save`, `memory_search`, `conversation_search`, `skill_read`, `routine_create`
(`{title, prompt, trigger?: "time" | "email" | "watch", repeat?, time?, weekday?, rule?}`; `rule` is required for `email` and `watch` and has the shapes above; still needs approval);
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

```ts
type ScheduledTask = {
  id: string; title: string; kind: "custom" | "brief"; prompt: string; mode: Mode; projectId: string | null
  trigger: "time" | "email" | "watch"                      // default "time"
  rule: EmailRule | WatchRule | null                        // null for "time"
  repeat: "hourly" | "daily" | "weekdays" | "weekly" | null // null unless trigger is "time"
  time: string | null; weekday: number | null              // "HH:MM" and 1-7; null unless trigger is "time"
  maxRunsPerHour: number                                    // default 10, 1 to 100
  enabled: boolean; lastRunAt: string | null; nextRunAt: string | null; createdAt: string // nextRunAt is null for email and watch
}
type EmailRule = { account?: string; from?: string; subject?: string; label?: string; hasAttachment?: boolean; smart?: string }
type WatchRule = {
  kind: "page" | "search"; url?: string; query?: string; selector?: string
  condition: "changed" | "below" | "above" | "contains" | "newItems"
  threshold?: number; text?: string; intervalMinutes: number // 1 to 1440, default 1
}
```
`time` and `weekday` are in the owner's `Preferences.timezone` (DST-aware; `weekdays` is Monday to Friday; hourly uses the minute of `time`).
Changing the time zone moves every enabled schedule's `nextRunAt` so it keeps its wall-clock time.
GET /schedules → `{schedules: ScheduledTask[]}`, POST /schedules, PATCH /schedules/:id, DELETE /schedules/:id, POST /schedules/:id/run (run now → `{id}` of the task).
A run creates a normal task with `scheduleId` set. Email and watch routines are started by the server instead of a clock (below). `title` is optional on create (default: the first 40 characters of the prompt, whitespace collapsed; at most 120 characters).
`POST /schedules {kind: "brief", time, repeat}` without `prompt` and `title` gets the title "Morning brief" and the server's brief prompt: Email (`gmail_digest` with `sinceLast`, only when Gmail is connected; item `detail` = sender), News (web search and fetch on `Preferences.newsTopics`, else general headlines; item `url` = article link) and Weather (for the time zone's main city, when reachable), published as one `feed` update with empty sections omitted.
A routine run has a lower step cap (`ROUTINE_MAX_STEPS`, default 40) and is told to end with exactly one `publish_update`. After three runs in a row end in `error` the routine is disabled (`enabled: false`, `nextRunAt: null`) and a `feed` update titled "<title> turned off" is added. A successful run resets the count; editing the routine resets it too.

### Email and watch routines

`trigger: "email"` runs when new mail matches `rule`. `account` limits it to one connected address (any account when absent); `from` and `subject` are case-insensitive substring matches and `label` is a case-insensitive match on a Gmail label id (`INBOX`, `STARRED`, `Label_12`); `hasAttachment` matches mail with or without attachments; `smart` is a plain-words condition the server asks the model about, one call per routine per batch of mail. At least one field other than `account` is required, and `prompt` is required. Mail sent from the account, drafts, and mail sent by Keen (header `X-Keen-Sent`) never match.
Each connected mailbox is checked about once a minute (spread per user). Mail that arrives while several checks are late is coalesced: one task per routine per check, whose prompt is the routine prompt plus the matching emails (account, id, from, subject, snippet) marked as untrusted data. The first check after a routine or account is added only records the mailbox position, so mail that arrived before it is not matched. If Gmail has dropped the history the server keeps, a `feed` update "Some email was not checked" is added and checking resumes from now. A 401 sets that account to `needs_auth` and stops its checks until it is reconnected.

`trigger: "watch"` runs when a web page or a search result changes. `kind: "page"` needs `url` (public http or https only; loopback, private, link-local, metadata and this server's own host are refused with 400). The refusal reads as a sentence the app can show: "invalid schedule: Keen can only watch public web pages. localhost is a private address." or, for a non-http address, "invalid schedule: Enter a full web address that starts with http:// or https://.". `kind: "search"` needs `query` and watches the set of result URLs. `selector` is a CSS selector for the part of a page to watch, otherwise the whitespace-normalized visible text is used. `below` and `above` need `threshold` and compare the first number in the value (currency symbols and thousands separators are tolerated); `contains` needs `text`; `newItems` fires on result URLs not seen before and is meant for `search`.
`below`, `above` and `contains` fire once when the condition becomes true and fire again only after it was false in between. `changed` and `newItems` fire on each new change and skip the first reading. A target shared by several routines (same kind, URL or query, selector) is fetched once per interval with `If-None-Match` and `If-Modified-Since`; a 304 changes nothing. Fetches time out after 10 seconds, are capped at 2 MB, follow redirects only to public addresses, are limited to one request per host per 10 seconds, and honour `Retry-After`.
A watch routine with an empty `prompt` publishes its update directly: importance `feed`, `title` = the routine title, `body` like "€129 → €118", `taskId: null`, `scheduleId` set. With a prompt it starts a task whose prompt carries the old and new values and the added and removed lines, marked as untrusted data. After three readings in a row where the selector matches nothing, one `feed` update "page layout changed" is added. After ten failed fetches in a row the routine is disabled and a `feed` update says so.

`maxRunsPerHour` caps the tasks (email) or fires (watch) a routine starts per hour. Further matches in that hour are dropped, and one `feed` update per hour says so.
Limits per user: 10 email routines and 20 watch routines. Creating or enabling past the limit returns 400 "at most 10 email routines" (or 20 watch).

`GET /schedules/:id/triggers?limit=30` (1 to 100) → `{triggers: {id, summary, taskId: string | null, createdAt}[]}`, newest first. Each entry is one fire (an email batch or a watch change). `taskId` is null when the update was written without a task. Another user's routine is 404.

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
  Built-in skills ship with the server (`bills-and-renewals`, `daily-briefing`, `expenses`, `follow-ups`, `inbox-triage`, `meeting-follow-up`,
  `meeting-prep`, `research-report`, `schedule-meeting`, `travel-itinerary`, `weekly-review`, `write-in-my-voice`; files in `internal/harness/skills`) and are not listed by GET /skills. A user skill with the same name replaces
  the built-in one. `/<name>` expands the user's skill, else the built-in one. The agent sees every skill's name and description in
  `skill_read {name}` and reads one before a matching task.
  Users never have to name a skill. POST /tasks only validates, stores and queues, and returns at once. At the task's
  first turn the worker builds the first message: for a prompt without a leading `/<name>`, a short model call picks up
  to 3 matching skills (or none) and their instructions go in front of it, then the project, knowledge and memory
  context; the task gets a `notify` event `Using skills: a, b`. A failed pick runs the task without skills. A routine's first run picks its skills and saves them
  on the schedule, and later runs reuse them; editing the prompt clears them so the next run picks again.
- `conversation_search {query}` (no approval) searches the user's other tasks: their messages, replies, the agent's questions,
  notes and results (not tool output). Up to 8 tasks, best match first, each with its date, first line, matching lines and last result.

## Connectors

```ts
type Connector = {
  name: string; kind: "mcp" | "gmail" | "calendar" | "contacts" | "drive"; url: string; status: "connected" | "needs_auth" | "error"
  error: string | null; tools: number
  account: string | null          // the primary Google address of a Google connector; null for mcp
  accounts: GmailAccount[]        // signed-in Google accounts of a Google connector; [] for mcp
}
type GmailAccount = { id: string; email: string; status: "connected" | "needs_auth" | "error"; error: string | null; primary: boolean }
```
A user can connect several Gmail accounts, and separately several Calendar, Contacts and Drive accounts. "Google connector" below means any of `gmail`, `calendar`, `contacts` and `drive`. The connector `status` is `connected` when any account is connected, `needs_auth` when every account needs auth, and `error` otherwise. A Gmail call Google rejects with 401, or a refresh
that fails, sets only that account to `needs_auth`; the tool result then reads "Gmail account <email> needs reconnecting in Connectors".
A failed authorization on an `mcp` connector sets the connector to `needs_auth`.
Accounts created before multi-account support are migrated to one primary account with their tokens and sync position.

| Method | Path | Body | Result |
|---|---|---|---|
| GET | /connectors | | `{connectors: Connector[]}`, including catalog connectors not added yet (`needs_auth`, or `error` "Not set up on this server" without a URL) |
| POST | /connectors | `{name, url}` | `Connector` (tries to connect; `needs_auth` if the server demands OAuth) |
| DELETE | /connectors/:name | | removes it and its tokens; for a Google connector it removes every account (for `contacts` also every stored contact) |
| POST | /connectors/:name/authorize | | `{authorizationUrl}` to open in a browser; adds a catalog connector on first use. For a Google connector it adds an account; signing in with an address that is already connected there reconnects that account |
| POST | /connectors/:kind/accounts/:id/authorize | | `{authorizationUrl}` to reconnect one account (`:kind` is `gmail`, `calendar`, `contacts` or `drive`). Signing in as a different address leaves the account in `error` with "signed in as a different account" |
| POST | /connectors/:kind/accounts/:id/primary | | 204; makes the account the primary |
| DELETE | /connectors/:kind/accounts/:id | | 204; revokes the token at Google (best effort) and removes the account (a contacts account also loses its stored contacts and sync state). Removing the primary promotes the oldest remaining account; removing the last account removes the connector |
| GET | /connectors/gmail/accounts/:id/labels | | `{labels: {id, name, type}[]}` from Gmail `users.labels.list`, in Gmail's order. `type` is `system` (`INBOX`, `STARRED`, ...) or `user`. Use `id` as an email rule's `label`. 404 for another user's account, a calendar account or a malformed id; 409 `NEEDS_AUTH` when the account must sign in again; 502 when Gmail fails |
| GET | /v1/oauth/callback?code&state | | no bearer auth; validates `state`, stores tokens, reconnects, then redirects to `keen://oauth?connector=<name>&status=connected\|error&account=<email>` (`account` only for Google connectors, the address that signed in) |

Another user's account id, or an account of another connector (a calendar account under `/connectors/gmail/...`, a drive account under `/connectors/contacts/...`, and so on), returns 404 on all three account routes.

The catalog is `gmail` (kind `gmail`), signed in with Google OAuth and served by the Gmail REST API directly (env `GMAIL_API_URL`,
default `https://gmail.googleapis.com`). The app offers only this one. It needs `GMAIL_CLIENT_ID` and `GMAIL_CLIENT_SECRET`;
`GMAIL_SCOPES` defaults to `gmail.modify gmail.compose`. The authorization URL adds `access_type=offline&prompt=consent` so Google
returns a refresh token, and carries no `resource` parameter. After sign-in the server reads the mailbox profile (the account's `email`).
Deleting the connector or an account revokes the token at Google first, best effort. Each account keeps its own sealed tokens and its own `sinceLast` position.

The catalog also has `calendar` (kind `calendar`), a separate Google sign-in served by the Calendar API v3 (env `CALENDAR_API_URL`,
default `https://www.googleapis.com`). `CALENDAR_CLIENT_ID` and `CALENDAR_CLIENT_SECRET` default to the Gmail ones; `CALENDAR_SCOPES`
defaults to `calendar.events calendar.calendarlist.readonly`. After sign-in the server reads the primary calendar, whose id is the
account's `email`. One address may be connected to both `gmail` and `calendar`; they are two accounts with their own tokens and
status. Removing one of them does not revoke at Google while the other still uses that address, since Google revokes the whole grant.
Calendar accounts never feed email routines or Gmail tools.

Every Gmail tool takes an optional `account` (a connected address). Without it:
- `gmail_search` and `gmail_digest` cover every connected account. Each message or item carries `account`. `gmail_search` returns `nextPageTokens` as `{<email>: token}` instead of `nextPageToken`; pass `account` with `pageToken` to continue one mailbox. An account that fails is listed in `errors` and the rest still answer.
- `gmail_get_message`, `gmail_get_thread`, `gmail_get_attachment`, `gmail_modify`, `gmail_trash` and `gmail_send_draft` act on one mailbox, so `account` is required when more than one account is connected.
- `gmail_create_draft`, `gmail_send`, `gmail_forward` and `gmail_list_labels` use the primary account. The `confirm` event input for the sending tools carries the resolved `account`.

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
| `gmail_create_draft` | `{to[], cc[], bcc[], subject, body, replyToMessageId?, replyAll?, attachments? (workspace paths; a path that leaves the workspace, also through a symlink, is refused; 25 MB in total)}` |
| `gmail_send` *! | same as `gmail_create_draft` |
| `gmail_send_draft` *! | `{draftId}` |
| `gmail_forward` *! | `{messageId, to[], cc[], note?}` with the original attachments |
| `gmail_modify` | `{ids[], addLabels[], removeLabels[], markRead?, archive?, star?}` |
| `gmail_trash` * | `{ids[]}` |

Calendar tools take an optional `account` too. Without it `calendar_list_calendars`, `calendar_list_events` and `calendar_free_busy`
cover every connected account (each item carries `account`; an account that fails is listed in `errors` as `{account, calendarId?, error}`
and the rest still answer). `calendar_create_event` uses the primary account. `calendar_get_event`, `calendar_update_event`,
`calendar_delete_event` and `calendar_respond` act on one account, so `account` is required when more than one is connected.
Times are RFC 3339; a date or a time without an offset is read in the user's `timezone` preference, and new events default to it.
A 401 or a refused refresh sets that account to `needs_auth` and the tool result reads "Calendar account <email> needs reconnecting in Connectors".

Calendar tools. `*` needs approval every call; `always` is allowed for them. The `confirm` event input is the resolved input:
`account` and `calendarId` (`primary`) filled in, `timeZone` added when `start` or `end` is given without one, and `sendUpdates`
(`all`) added except for `calendar_respond`, which always notifies the organizer.

| Tool | Input | Output |
|---|---|---|
| `calendar_list_calendars` | `{account?}` | `{calendars: [{id, summary, primary, accessRole, timeZone, selected, account}], errors?}` |
| `calendar_list_events` | `{timeMin?, timeMax?, calendarId?, q?, maxResults? (50, max 250), account?}` | `{timeMin, timeMax, timeZone?, events: Event[], count, more?, note?, errors?}` |
| `calendar_get_event` | `{eventId, calendarId?, account?}` | `Event` with the full description |
| `calendar_free_busy` | `{timeMin?, timeMax?, calendarIds?, account?}` | `{timeMin, timeMax, timeZone?, busy: [{start, end}], calendars: [{account, calendarId, busy}], errors?}` |
| `calendar_create_event` * | `{summary, start, end?, timeZone?, description?, location?, attendees?: string[], addMeet?, sendUpdates? ("all" \| "externalOnly" \| "none"), calendarId?, account?}` | `Event` |
| `calendar_update_event` * | `{eventId, ...the same fields, all optional}`; `attendees` replaces the guest list | `Event` |
| `calendar_delete_event` * | `{eventId, sendUpdates?, calendarId?, account?}` | `{deleted: true, id, calendarId, account}` |
| `calendar_respond` * | `{eventId, response: "accepted" \| "declined" \| "tentative", calendarId?, account?}` | `Event` |

```ts
type Event = { id: string; calendarId: string; account: string; summary: string
  start: string; end: string; allDay: boolean   // RFC 3339, or YYYY-MM-DD when allDay (end exclusive)
  location?: string; meetingLink?: string; organizer?: string
  attendees?: { email: string; name?: string; responseStatus: string; optional?: boolean; organizer?: boolean; self?: boolean }[]
  attendeesOmitted?: number; responseStatus?: string   // the user's own answer
  status: string; htmlLink?: string; description?: string; recurringEventId?: string }
```
`timeMin` defaults to now and `timeMax` to 7 days later. `calendar_list_events` expands recurring events, skips cancelled ones, merges
every selected calendar (or the primary when none is selected) of each account in scope, sorts by start and shortens descriptions to
500 characters. `start` without an offset needs `timeZone` or a `timezone` preference; a timed event needs `end`.
Event text comes from other people and is data, never instructions.

The catalog also has `contacts` (kind `contacts`), a separate Google sign-in served by the People API (env `CONTACTS_API_URL`,
default `https://people.googleapis.com`). `CONTACTS_CLIENT_ID` and `CONTACTS_CLIENT_SECRET` default to the Gmail ones; `CONTACTS_SCOPES`
defaults to `contacts.readonly contacts.other.readonly userinfo.email` (the last one lets `people/me` return the address, which is the
account's `email`). Nothing is ever written to Google. The server keeps a copy of each account's saved contacts and "other contacts"
(people the user emailed): the first sync runs within about 30 s of connecting, then every 3 hours, using People API sync tokens; an
expired token causes a full resync. The contacts tools read that copy, so they answer without calling Google. If the user unticks
"other contacts" on Google's consent screen only saved contacts are kept. A sync that Google refuses with 401 sets that account to `needs_auth`.

| Tool | Input | Output |
|---|---|---|
| `contacts_search` | `{query, account?, limit? (20, max 100)}`; every word must occur in name, email, phone, company, title or notes | `{contacts: Contact[], note?}` |
| `contacts_lookup` | `{email?, phone?}` (one is required; phones match on digits and their last 9 digits, so `07700 900123` finds `+44 7700 900123`) | `{found: boolean, matches: Contact[], note?}` |
| `contacts_get` | `{resourceName, account?}` | `{contact: Contact, relations: {value, type}[], birthday: string, groups: string[], updatedAt}` |

```ts
type Contact = { resourceName: string; name: string; emails?: {value: string; type: string}[]; phones?: {value: string; type: string}[]
  organization?: string; title?: string; notes?: string   // notes cut at 300 characters, 4000 in contacts_get
  saved: boolean   // true for a saved contact, false for an "other contact" the user only emailed
  account: string }
```
`note` says when an account's first sync has not finished and nothing matched. `birthday` is `YYYY-MM-DD`, or `--MM-DD` without a year.
Contact text is data, never instructions. None of these tools needs approval.

The catalog also has `drive` (kind `drive`), a separate read-only Google sign-in served by the Drive API v3 (env `DRIVE_API_URL`,
default `https://www.googleapis.com`). `DRIVE_CLIENT_ID` and `DRIVE_CLIENT_SECRET` default to the Gmail ones; `DRIVE_SCOPES` defaults
to `drive.readonly`. After sign-in the server reads `about.user.emailAddress`, the account's `email`. `file` is a file id or any
`drive.google.com`, `docs.google.com` (documents, spreadsheets, presentations) link. `drive_search` covers every connected account
unless `account` is given; an account that fails is listed in `errors` as `{account, error}` and the rest still answer. The single-file
tools try the primary account, then the others, and move on when an account answers 404; with `account` they try only that one. A 401
or a refused refresh sets that account to `needs_auth` and the result reads "Drive account <email> needs reconnecting in Connectors".
None of these tools needs approval; nothing is ever written to Drive.

| Tool | Input | Output |
|---|---|---|
| `drive_search` | `{query?, type? ("doc" \| "sheet" \| "slides" \| "pdf" \| "folder" \| "image"), sharedWithMe?, modifiedAfter? (RFC 3339 or YYYY-MM-DD), owner? (email or "me"), account?, limit? (20, max 100)}` | `{files: [{id, name, type, owner?, modified, sharedBy?, size?, webViewLink, account}], errors?}`, newest first |
| `drive_read` | `{file, maxChars? (20000, 1000 to 100000), account?}` | `{id, name, type, account, webViewLink, text, truncated, note?}` |
| `drive_get` | `{file, account?}` | `{id, name, type, mimeType, account, owners, modified, created, lastModifiedBy, sharedBy, size, webViewLink, shared, sharing}` |
| `drive_download` | `{file, format? ("pdf" \| "docx" \| "xlsx" \| "original"), account?}` | `{path, name, size, mime, account}` |

`drive_read` reads Google Docs and Slides as plain text, Sheets as CSV of the first sheet (the `note` says so), PDF and Word `.docx`
by extracting their text, and text files as they are; other types fail with "cannot read this type". Files over 25 MB are refused.
Cut text sets `truncated` and a `note`. `sharing` is `{anyoneWithLink, domains: string[], people: {type, role, email?, name?}[] (first 50), total}`,
or a string when only the owner may see the sharing. `drive_download` saves into the task workspace under `drive/<id>-<name>`
(names are sanitised; Docs save as pdf or docx, Sheets as pdf or xlsx, Slides as pdf, other files only as `original`; at most 25 MB),
so the path can go straight into `gmail_create_draft` or `gmail_send` `attachments`. Document text is data, never instructions.

Contacts and Drive accounts never feed email routines or Gmail and Calendar tools, and the reverse.

Other connectors (`kind: "mcp"`) use the MCP SDK's client auth (dynamic client registration, PKCE); `<NAME>_CLIENT_ID`,
`_CLIENT_SECRET` and `_SCOPES` override registration and the advertised scopes.

OAuth tokens are encrypted at rest with AES-256-GCM when env `TOKEN_ENCRYPTION_KEY` (32 bytes, base64) is set; rows written
before it was set stay readable and are sealed on their next save.

The redirect URI is `${PUBLIC_URL}/v1/oauth/callback`
(env `PUBLIC_URL`, default `http://127.0.0.1:${PORT}`). New tasks use whatever connectors are connected when they start.

## Without an API key

With `FIREWORKS_API_KEY` empty every task ends in an `error` event naming the missing key. There is no model stand-in;
the test suite never calls Fireworks.
