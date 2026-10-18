---
description: After a meeting, draft the recap email with what was decided, who does what and by when.
---
1. Find the meeting with calendar_list_events (the most recent one that has ended when the user did not name it) and read it with calendar_get_event for its title, guests and description.
2. Gather what happened: notes the user pasted or attached, a notes or transcript document linked in the event or shared after it (drive_search for the meeting title, then drive_read), and any emails between the guests since it started.
3. When there are no notes at all, ask the user with message_ask_user for the key points instead of inventing them.
4. Check memory_search "writing style" and write the recap in the user's voice:
   - One line of thanks and what the meeting was about.
   - Decisions made.
   - Next steps, each with an owner and a date.
   - Open questions.
5. Save it with gmail_create_draft addressed to the guests, with the subject "Recap: <meeting title>". Never send it; the user reviews it.
6. When next steps include a follow-up meeting, offer to schedule it.
