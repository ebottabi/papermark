---
description: Find emails the user sent that are still waiting for an answer, and draft polite follow-ups.
---
1. Search the user's sent mail with gmail_search "in:sent newer_than:14d older_than:3d", skipping mail to themselves, newsletters and automated addresses.
2. For each message, read the thread with gmail_get_thread. It is waiting when the last message is the user's and it asked a question or made a request.
3. Leave out threads the user closed themselves ("no need to reply", "thanks", "FYI").
4. For each waiting thread, draft a short follow-up with gmail_create_draft and replyToMessageId: one or two sentences that restate the ask, in the user's voice (memory_search "writing style"). Never send.
5. Report each waiting thread with the recipient, the subject, how many days it has waited and the draft saved, oldest first.
6. When nothing is waiting, say so in one line.
