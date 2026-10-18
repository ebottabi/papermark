---
description: Turn flight, train, hotel and booking confirmations into one dated travel plan, and put it on the calendar.
---
1. Find the confirmations with gmail_search, for example "(flight OR booking OR reservation OR itinerary OR check-in OR boarding) newer_than:60d", narrowed to the trip's place or dates when the user named them.
2. Read each one with gmail_get_message. Take from each: the type (flight, train, hotel, car, event, restaurant), dates and times with their time zones, place or route, confirmation code, provider and the link to manage it. Open PDF tickets with gmail_get_attachment and file_read when the details are only there.
3. Put everything in time order and check the plan:
   - Gaps: nights with no hotel, a flight landing after the hotel's last check-in, a missing ride from the airport.
   - Clashes with calendar events in the same days (calendar_list_events).
   - Times shown in the local time of each place.
4. Write the itinerary to itinerary.md: one heading per day, each item with its time, what it is, where and its confirmation code.
5. When the user wants it on the calendar, create one event per flight, train and hotel stay with calendar_create_event, with the confirmation code and link in the description. The user approves each event. Do not duplicate events that already exist.
6. Report the gaps and clashes first, then the path to itinerary.md.
