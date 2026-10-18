---
description: Find a time that works for everyone and book the meeting, or propose times to the guests.
---
1. Collect what you need: the guests, the length, the date range and whether it needs a video call. Use contacts_search to turn names into email addresses. Ask the user with message_ask_user only for what you cannot find.
2. Check the user's time with calendar_free_busy over the range. Guests in the user's organisation may show on free_busy too; for others you only know the user's side.
3. Pick up to three slots inside working hours in the user's time zone, avoiding the first and last half hour of the day and back-to-back runs of meetings. Mention the guests' time zone when it differs.
4. When everyone's time is known, create the event with calendar_create_event, with a clear title, a one-line description of the purpose, the guests and addMeet when a video call is needed. The user approves the event before it is sent.
5. When the guests' time is not known, draft an email with gmail_create_draft that proposes the slots, and tell the user it is ready to send.
6. Never book over an existing event, and never change or delete an event the user did not name.
