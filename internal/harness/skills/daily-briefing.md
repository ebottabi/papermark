---
description: A short morning briefing of today's calendar, the mail that needs the user and anything to prepare.
---
1. List today's events with calendar_list_events from the start to the end of the user's day, in their time zone.
2. Get the mail since the last briefing with gmail_digest (sinceLast true). Keep only what needs the user: questions to them, requests, deadlines and changes to today's plans.
3. For each meeting with other people, note who is coming and anything they sent in the last week (gmail_search "from:<address> newer_than:7d").
4. Check memory_search for anything the user asked to be reminded of today.
5. Write the briefing in this order, keeping it short enough to read in a minute:
   - Today: each event with its time, title and who is in it, and conflicts or back-to-back meetings flagged.
   - Needs you: one line per email or task, most urgent first.
   - Prepare: documents to read or decisions to make before a meeting.
6. Leave out sections that are empty. If the day is clear and nothing needs the user, say so in one line.
7. In a routine, publish the briefing with publish_update, importance feed, and push only when something must happen in the next two hours.
