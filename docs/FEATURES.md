# Slacker feature checklist

Legend: `[x]` available, `[~]` partial, `[ ]` planned, `[-]` better opened in a browser.

## Navigation and conversations

- [x] One active workspace with CLI workspace switching
- [x] Public channels, private channels, DMs, and group DMs
- [x] Slack custom sidebar sections and channel order
- [x] Live message updates over RTM with reconnect backoff
- [x] Channel history and channel message sending
- [x] Thread history and thread replies
- [x] DM display names and message author names
- [~] Unread and mention markers
- [x] Slack-style right-hand thread pane
- [ ] Mark channel/thread read and preserve read position
- [ ] More-unreads navigation and unread-only view
- [ ] Browse/search/join/leave/create/archive channels
- [~] Start one-to-one DMs from profiles; group DM creation remains planned
- [ ] Channel topics, purposes, bookmarks, tabs, and member lists
- [ ] Conversation history pagination and jump to date/message

## Messages

- [x] Plain-text channel messages and replies
- [~] Slack mrkdwn display
- [x] Edit and delete your own messages
- [ ] Rich composer: bold, italic, strike, code, quotes, lists, and links
- [ ] Mentions and channel/user/emoji autocomplete
- [ ] Scheduled messages and drafts
- [ ] Forward/share messages and copy permalinks
- [ ] Pin/unpin, save-for-later, reminders, and follow-thread controls
- [~] Keyboard message actions; Slack app/workflow actions remain planned
- [ ] Translation and accessibility alt-text prompts

## Reactions and emoji

- [x] Display reaction names, counts, and whether you reacted
- [x] Add common reactions and remove your own reactions
- [ ] Full emoji picker and reacting-user list
- [ ] Workspace emoji, aliases, skin tones, and emoji picker

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
- [~] Inline RGB-cell image previews; native Kitty graphics, iTerm2 images, and Sixel remain planned
- [x] File rows identify images and open them in the system image viewer
- [ ] Text fallback for terminals without graphics support
- [ ] Image galleries, video/audio playback handoff, clips, and transcripts
- [ ] Files view and file search

## Search and activity

- [~] Message and file search; channel/people search and richer filters remain planned
- [ ] Activity/mentions/reactions/invites view
- [ ] Threads view, saved items, reminders, and later/to-do view
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
- [ ] Local cache for instant startup, offline history, and reduced API traffic
