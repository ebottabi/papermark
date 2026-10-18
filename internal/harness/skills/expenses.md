---
description: Collect receipts from email into a spreadsheet of expenses with the receipts saved alongside.
---
1. Agree the period and scope: the dates, and whether it is all spending or only work expenses. Default to last month, all receipts.
2. Find the receipts with gmail_search, for example "(receipt OR invoice OR \"order confirmation\" OR \"payment received\") after:YYYY/MM/DD before:YYYY/MM/DD", and read each with gmail_get_message.
3. For each receipt, take the date, vendor, amount, currency, what it was for and whether it was refunded. When the amount is only in a PDF, save it with gmail_get_attachment and read it with file_read.
4. Save the receipt files into the workspace folder receipts/, named date-vendor-amount.
5. Write expenses.csv with the columns date, vendor, description, amount, currency, category, receipt_file, email_link. Categories: travel, meals, software, office, transport, other. Do not convert currencies unless asked; give a total per currency.
6. Leave out refunded orders and duplicates (the order and the shipping email for the same purchase).
7. Report the totals per currency and category, anything unclear (no amount, unknown currency), and the paths to expenses.csv and receipts/.
