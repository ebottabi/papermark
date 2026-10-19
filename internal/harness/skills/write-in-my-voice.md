---
description: Learn how the user writes. Measure their style per audience from sent mail, save a compact voice profile, and check drafts against it so they sound like the user.
---
Sent mail is personal data. Read it to learn the style, never to save its contents.

## 1. Start from what is known
- memory_search "Writing style". When a profile exists and the user didn't ask to redo it, show it, then ask what to change. Update it rather than starting over.

## 2. Sample
- Use gmail_search "in:sent newer_than:180d" and read 25 to 30 of the user's own messages with gmail_get_message, so quoted history is removed. Mix three groups:
  - close colleagues or friends
  - external or formal contacts, such as clients, vendors and strangers
  - short replies and longer messages
- Skip messages that were forwarded, written from templates, auto-generated, mostly someone else's quoted text, or written in another language. When the user writes in more than one language, profile each language separately.

## 3. Measure
These are the things a voice is made of, so record each as an observation, not a feeling.
- **Openers and sign-offs.** The exact words ("Hi Ana,", "Hey,", none) and the sign-off ("Best,", "Thanks!", just their name), per audience.
- **Length.** The typical word count for a reply and for a new message.
- **Sentences.** Short or long, and whether they vary. Use of fragments.
- **Register.** Contractions, how formal they are, and whether they write "I" or "we".
- **Structure.** The ask first or the context first. Bullets or prose. One question per mail or several.
- **Habits.** Their actual words and phrases, emoji, exclamation marks, punctuation quirks, capitalisation, and British or American spelling.
- **Never.** Things they never do, such as never "Dear", never "I hope this finds you well", never apologising for a late reply. This list matters as much as the habits.

## 4. Save
memory_save one memory, under 800 characters, that starts with "Writing style:". Use a compact form like this:
```
Writing style: Replies 30-60 words, new mail ~100. Opens "Hi <first name>," (formal: "Hello <name>,"), close friends no greeting. Signs "Best, Sam", friends "S". Ask first, context after. Short sentences, contractions, no exclamation marks, rare emoji (only 🙏 with friends). British spelling. Uses "happy to", "shout if". Never: "Dear", "hope you're well", "per my last email", apologising for delays.
```
- Save no names other than the user's own, no addresses, and no content from any email.

## 5. Show and confirm
- Write two sample replies with the profile, one casual and one formal, to an invented ordinary request.
- Ask the user whether they sound right. Apply their corrections to the profile and save it again. The newest profile wins.

## 6. When drafting with the profile
Any skill drafting for the user should check the draft against this profile before saving it:
- the greeting and sign-off fit the audience
- the length is within range
- the ask comes in the user's usual position
- nothing from the Never list appears

Whenever the user edits a draft and says why ("too formal", "I never say that"), update the profile.
