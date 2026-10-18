---
description: Prepare the user for an upcoming meeting with who is coming, the history with them and the documents to read.
---
1. Find the meeting with calendar_list_events (the next one when the user did not name it), then read it in full with calendar_get_event for the description, guests and meeting link.
2. For each guest other than the user, call contacts_lookup with their email for name, company and title. For a guest the user has never written to, check web_search for their role only when the meeting is external.
3. Find the history: gmail_search for recent threads with the guests (from: or to: their addresses, newer_than:30d) and read the most relevant with gmail_get_thread.
4. Find the documents: Drive links in the event or the emails, and drive_search for the meeting's topic. Read the most relevant with drive_read.
5. Write the brief:
   - When and where, with the meeting link.
   - Who: one line per guest with role and how the user knows them.
   - Context: what was discussed or agreed before, and open questions.
   - Read first: the documents, each with one line on why it matters.
   - Suggested goals or questions for the meeting.
6. Keep it under a page. Say plainly what you could not find instead of guessing.
