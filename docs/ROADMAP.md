# Agent roadmap

Features Hermes Agent and OpenClaw have that we do not, in build order. Each item names what the user notices.

## Done

- **Built-in skills.** Twelve task playbooks ship with the server (`internal/harness/skills`). The agent sees them in
  `skill_read` and follows one before a matching task. A user skill with the same name replaces the built-in one.
- **Automatic skill choice.** Every new task gets up to 3 matching skills picked by a short model call, injected into
  its first message. Routines save their pick and reuse it until the prompt changes.
- **Search across past conversations.** `conversation_search` finds the user's earlier tasks, so "like last time" and
  "what did we decide about X" work.

## Next

1. **Skills the agent writes.** After a task succeeds, the agent saves or improves a skill (Hermes does this). It needs
   approval, a cap on size and a way for the user to see and undo the change. Builds on the skills table.
2. **Read a past task in full.** `conversation_search` returns snippets and results. A follow-up tool would return the whole
   transcript of one task when a snippet is not enough.
3. **Heartbeat.** The agent wakes on a schedule (OpenClaw: every 30 minutes), checks a standing list and acts only when
   something needs it. Needs a token budget per day, because idle checks cost money.
4. **Phone as a device.** Location, on-device reminders, photos and the share sheet as approved tools, through the iOS app
   (OpenClaw "nodes"). We own the app, so this is where we can do better than both.
5. **Reaching the agent outside the app.** Email or Slack in and out first, then Telegram and WhatsApp (both have a gateway
   for many chat apps).
6. **A real browser.** Pages that need JavaScript, logins or forms. `web_fetch` only reads static pages.
7. **Parallel sub-agents.** Side tasks run at the same time, for research-heavy work.
8. **Voice.** A voice note becomes a task; spoken replies.
9. **Installable skills.** Users share and install skill files. Only after item 1.
10. **Built-in skills in the app.** GET /skills lists only the user's skills. The iOS skills screen could show the built-in
    ones too, so users can find and copy them.
