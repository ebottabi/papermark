---
description: Look back at the user's past week and ahead to the next one, with what slipped and what needs preparing.
---
1. List last week's events (Monday to Sunday in the user's time zone) with calendar_list_events, and next week's the same way.
2. Find what the user sent last week with gmail_search "in:sent newer_than:7d", and the open threads: mail to the user from the last 7 days that asks them something and has no reply from them yet (gmail_get_thread to check).
3. Look for what slipped: meetings that were cancelled or moved, promises the user made in their sent mail ("I'll send", "by Friday") without a later message that keeps them.
4. For next week, flag days with more than five hours of meetings, conflicts, meetings with no agenda and deadlines mentioned in mail.
5. Write the review in this order, short enough to read in two minutes:
   - Last week: the main meetings and what came of them, in a few lines.
   - Still open: each unanswered thread and slipped promise, oldest first.
   - Next week: the busy days, the important meetings and what to prepare for them.
6. Leave out empty sections. In a routine, publish it with publish_update, importance feed.
