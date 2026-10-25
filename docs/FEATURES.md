# Slacker feature checklist

Legend: `[x]` available, `[~]` partial, `[ ]` planned, `[-]` better opened in a browser.

## Navigation and conversations

- [x] One active workspace with CLI workspace switching
- [x] Public channels, private channels, DMs, and group DMs
- [x] Slack custom sidebar sections and channel order
- [x] Live message updates over RTM with reconnect backoff
- [x] Channel history and channel message sending
- [x] Thread history and thread replies
- [x] DM display names and message author names with background resolution and persistent profile caching
- [x] Live unread and mention markers
- [x] Slack-style right-hand thread pane
- [x] Mark conversations read on open and update every sidebar view
- [x] Unread-only view with mention-first ordering and `u` next-unread navigation
- [ ] Browse/search/join/leave/create/archive channels
- [~] Start one-to-one DMs from profiles; group DM creation remains planned
- [ ] Channel topics, purposes, bookmarks, tabs, and member lists
- [~] Upward conversation history pagination; jump to date/message remains planned

## Messages

- [x] Plain-text channel messages and replies
- [x] Slack mrkdwn display for emphasis, strike, inline/fenced code, quotes, links, mentions, channels, dates, and entities
- [x] Edit and delete your own messages
- [~] Rich composer: Slack mrkdwn, multiline input, and inline completion work; formatting toolbar remains planned
- [~] Tab completion for known users, channels, standard emoji, and cached workspace emoji; popup selection remains planned
- [~] In-session channel and thread drafts; persistence and scheduled messages remain planned
- [~] Copy message permalinks; forwarding/sharing remains planned
- [~] Pin/unpin messages; save-for-later, reminders, and follow-thread controls remain planned
- [~] Keyboard message actions; Slack app/workflow actions remain planned
- [ ] Translation and accessibility alt-text prompts

## Reactions and emoji

- [x] Display reaction names, counts, whether you reacted, and enlarged custom reaction artwork
- [x] Add and remove reactions with a searchable standard/workspace emoji picker
- [x] Click a reaction to list reacting members and open their profiles
- [~] Standard Unicode emoji and workspace aliases render inline; custom workspace emoji render on demand through Kitty graphics, GIF emoji animate with a Ghostty client-driven fallback, and emoji-only custom messages use jumbo rendering; skin-tone selection remains planned

## People and presence

- [x] Clickable profiles with names, title, pronouns, status, contact details, timezone/local time, roles, and custom workspace fields
- [~] Presence, notification snooze, and last-activity data; typing indicators remain planned
- [ ] Profile photos and profile editing
- [ ] Set/clear status, availability, and notification pause
- [ ] User groups and workspace directory

## Files and media

- [~] File names and MIME types in messages
- [x] Authenticated file download and default-viewer open actions
- [ ] File upload, drag/drop, clipboard paste, and filename anonymisation
- [~] Inline Kitty-protocol images; iTerm2 images, Sixel, and non-graphics fallbacks remain planned
- [x] File rows identify images and open them in the system image viewer
- [ ] Text fallback for terminals without graphics support
- [ ] Image galleries, video/audio playback handoff, clips, and transcripts
- [ ] Files view and file search

## Search and activity

- [~] Message and file search; channel/people search and richer filters remain planned
- [~] Activity view with mentions, thread replies, reactions, DMs, channel notifications, reminders, and direct navigation; restricted workspaces use an unread-messages-and-DMs compatible view
- [~] Unread Threads view with direct thread opening; read threads, saved items, reminders, and later/to-do view remain planned
- [ ] Search history and keyboard quick switcher

## Calls and collaboration

- [ ] Huddle presence, participants, join/leave, mute, and reactions
- [-] Huddle audio/video, screen sharing, and Slack Calls media
- [ ] Canvas viewing and links
- [-] Full Canvas editing, Lists, Workflow Builder, and advanced app surfaces

## Notifications and preferences

- [ ] Desktop notifications, sounds, badges, and click-to-open
- [ ] Per-channel notification preferences and mute controls
- [ ] Do-not-disturb schedules and snooze
- [ ] Streamer mode, silent typing, URL cleaning, private-channel display, and filename privacy
- [ ] Theme, density, key bindings, accessibility, and feature settings UI

## Workspace and account

- [x] Browser-session authentication as the user
- [x] OS-keyring credential storage
- [x] Multiple saved workspaces with one active workspace
- [ ] Seamless session refresh and account/workspace picker
- [ ] Workspace preferences, invites, members, roles, and administration
- [ ] Slack Connect invitations and external-organization indicators
- [ ] Enterprise Grid organization navigation

## Terminal and distribution

- [x] Keyboard and mouse navigation
- [x] macOS, Linux, and Windows builds
- [x] CI and release packaging foundation
- [ ] Homebrew, APT, RPM, AUR, Nix, Scoop, WinGet, and FreeBSD publication
- [ ] Shell completion, man pages, update checking, and diagnostics bundle
- [~] Local cache for images, DM activity, and resolved user profiles; offline message history remains planned
