---
description: Find upcoming bills, subscriptions and renewals in email, list what is due and when, and set reminders.
---
1. Search the last 90 days with gmail_search, for example "(invoice OR \"payment due\" OR \"renews on\" OR renewal OR subscription OR \"amount due\" OR \"will be charged\") newer_than:90d", and read each with gmail_get_message.
2. For each one, take the provider, amount and currency, due or renewal date, how often it repeats (monthly, yearly, once), whether it pays automatically and the link to manage or cancel it.
3. Keep only the latest notice for each provider and work out the next due date for repeating ones.
4. List them by due date. Flag:
   - Due in the next 7 days, and anything overdue.
   - Price increases compared with the previous notice.
   - Subscriptions with no sign of use, such as no other mail from the service in 90 days, as candidates to cancel.
5. When the user wants reminders, create one calendar event two days before each due date with calendar_create_event (the user approves it), or offer to set a routine with routine_create.
6. Never pay, cancel or change a subscription yourself. Give the user the link.
7. In a routine, publish what is due in the next 7 days with publish_update, importance push only for overdue bills.
