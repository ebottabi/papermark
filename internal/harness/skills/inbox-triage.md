---
description: Sort new email into what needs the user, what can wait and what is noise, and draft the replies that are due.
---
1. Get the new mail with gmail_digest (sinceLast true in a routine), or gmail_search "is:unread in:inbox newer_than:2d" when asked about the inbox now.
2. For each sender you do not recognise, call contacts_lookup. Mail from a saved contact or a person the user writes to matters more than mail from a company.
3. Put every message in one group:
   - Needs the user: a question to them, a request, a deadline, a meeting change, a payment or security notice.
   - Can wait: updates and threads where the user is only copied.
   - Noise: newsletters, promotions, automated notifications.
4. Read the full thread (gmail_get_thread) only for messages that need the user, so the summary is right.
5. For each message that needs a reply, write it with gmail_create_draft and replyToMessageId. Write in the user's voice: check memory_search "writing style", else match their earlier replies in the thread. Never send; the user reviews the drafts.
6. Only when the user asked for it, archive noise with gmail_modify. Never trash mail.
7. Report the needs-the-user group first, one line each with sender, subject and what is asked, then the drafts you saved, then a count of the rest. In a routine, put that report in publish_update, with importance push only when something needs the user today.
