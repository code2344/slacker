package root

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/flex"
	"github.com/ayn2op/tview/layers"
	"github.com/ayn2op/tview/modal"
	"github.com/ayn2op/tview/tree"
	"github.com/code2344/slacker/internal/config"
	"github.com/code2344/slacker/internal/slack"
	"github.com/code2344/slacker/internal/ui"
	"github.com/gdamore/tcell/v3"
)

// Model is the first Slack-backed shell of the three-pane interface.
// Message loading and the composer are wired in subsequent backend slices.
type Model struct {
	*layers.Layers
	cfg                *config.Config
	sidebar            *tree.Model
	messages           *tree.Model
	composer           *tview.TextArea
	main               *flex.Model
	right              *flex.Model
	inspector          *flex.Model
	threadMessages     *tree.Model
	threadComposer     *tview.TextArea
	inspectorVisible   bool
	client             *slack.Client
	users              map[string]slack.User
	counts             map[string]slack.ConversationCount
	conversations      map[string]slack.Conversation
	activeConversation string
	activeThread       string
	selfUserID         string
	editing            *messageReference
	editingInThread    bool
	pendingAction      string
	pendingMessage     *messageReference
	stream             *slack.Realtime
	realtimeFailures   int
}

type historyLoadedMsg struct {
	conversation slack.Conversation
	messages     []slack.Message
	hasMore      bool
	err          error
}

type messageSentMsg struct {
	conversation slack.Conversation
	thread       string
	err          error
}

type messageMutatedMsg struct {
	conversation slack.Conversation
	thread       string
	action       string
	err          error
}

type messageReference struct {
	conversation  slack.Conversation
	message       slack.Message
	contextThread string
}
type profileReference struct{ userID string }
type threadBackReference struct{ conversation slack.Conversation }
type threadLoadedMsg struct {
	conversation slack.Conversation
	timestamp    string
	messages     []slack.Message
	err          error
}

type realtimeConnectedMsg struct {
	stream *slack.Realtime
	err    error
}
type realtimeEventMsg struct {
	event slack.RealtimeEvent
	err   error
}

func NewModel(cfg *config.Config, workspaceName, selfUserID string, conversations []slack.Conversation, sections []slack.ChannelSection, users map[string]slack.User, counts map[string]slack.ConversationCount, client *slack.Client) *Model {
	m := &Model{
		Layers:         layers.New(),
		cfg:            cfg,
		sidebar:        tree.NewModel(),
		messages:       tree.NewModel(),
		composer:       tview.NewTextArea(),
		main:           flex.NewModel(),
		right:          flex.NewModel(),
		inspector:      flex.NewModel(),
		threadMessages: tree.NewModel(),
		threadComposer: tview.NewTextArea(),
		client:         client,
		selfUserID:     selfUserID,
		users:          users,
		counts:         counts,
		conversations:  make(map[string]slack.Conversation, len(conversations)),
	}
	m.build(workspaceName, conversations, sections)
	return m
}

func (m *Model) build(workspaceName string, conversations []slack.Conversation, sections []slack.ChannelSection) {
	sections = orderSections(sections)
	root := tree.NewNode("")
	channels := tree.NewNode("Channels").SetExpanded(true).SetSelectable(false)
	dms := tree.NewNode("Direct messages").SetExpanded(true).SetSelectable(false)
	sectionNodes := map[string]*tree.Node{}
	assigned := map[string]bool{}
	for _, section := range sections {
		name := section.Name
		if name == "" {
			name = strings.Title(strings.ReplaceAll(section.Type, "_", " "))
		}
		node := tree.NewNode(name).SetExpanded(true).SetSelectable(false)
		root.AddChild(node)
		sectionNodes[section.ID] = node
		for _, id := range section.ChannelIDsPage.ChannelIDs {
			assigned[id] = true
		}
	}
	if len(sections) == 0 {
		root.AddChild(channels).AddChild(dms)
	}

	byID := make(map[string]slack.Conversation, len(conversations))
	for _, conversation := range conversations {
		m.conversations[conversation.ID] = conversation
		byID[conversation.ID] = conversation
	}
	addConversation := func(parent *tree.Node, conversation slack.Conversation) {
		name := conversation.Name
		prefix := "# "
		if conversation.IsIM || conversation.IsMPIM {
			prefix = "@ "
			if user, ok := m.users[conversation.User]; ok {
				name = displayName(user)
			}
		} else if conversation.IsPrivate {
			prefix = "🔒 "
		}
		if name == "" {
			name = conversation.ID
		}
		if count := m.counts[conversation.ID]; count.UnreadCount > 0 || count.MentionCount > 0 || (count.Latest != "" && count.Latest > count.LastRead) {
			prefix = "● " + prefix
			if count.MentionCount > 0 {
				name += fmt.Sprintf(" (%d)", count.MentionCount)
			}
		}
		parent.AddChild(tree.NewNode(prefix + name).SetReference(conversation.ID))
	}
	for _, section := range sections {
		parent := sectionNodes[section.ID]
		for _, id := range section.ChannelIDsPage.ChannelIDs {
			if conversation, ok := byID[id]; ok {
				addConversation(parent, conversation)
			}
		}
	}
	var remaining []slack.Conversation
	for _, conversation := range conversations {
		if !assigned[conversation.ID] {
			remaining = append(remaining, conversation)
		}
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].Name < remaining[j].Name })
	for _, conversation := range remaining {
		if len(sections) > 0 {
			if conversation.IsIM || conversation.IsMPIM {
				if dms.Children() == nil {
					root.AddChild(dms)
				}
				addConversation(dms, conversation)
			} else {
				if channels.Children() == nil {
					root.AddChild(channels)
				}
				addConversation(channels, conversation)
			}
		} else if conversation.IsIM || conversation.IsMPIM {
			addConversation(dms, conversation)
		} else {
			addConversation(channels, conversation)
		}
	}

	m.sidebar.SetRoot(root).SetTopLevel(1).SetTitle(workspaceName)
	ui.ConfigureBox(m.sidebar.Box, &m.cfg.Theme)
	ui.FocusBox(m.sidebar.Box, &m.cfg.Theme)

	m.messages.SetRoot(tree.NewNode("").AddChild(tree.NewNode(fmt.Sprintf("Connected to %s — select a conversation to begin.", workspaceName)).SetSelectable(false))).SetTopLevel(1).SetTitle("Messages")
	ui.ConfigureBox(m.messages.Box, &m.cfg.Theme)

	m.composer.SetPlaceholder(nil)
	m.composer.SetDisabled(true)
	m.composer.SetTitle("Composer")
	ui.ConfigureBox(m.composer.Box, &m.cfg.Theme)

	m.right.SetDirection(flex.DirectionRow).
		AddItem(m.messages, 0, 1, false).
		AddItem(m.composer, 3, 0, true)
	m.threadMessages.SetRoot(tree.NewNode("")).SetTopLevel(1).SetTitle("Thread")
	ui.ConfigureBox(m.threadMessages.Box, &m.cfg.Theme)
	m.threadComposer.SetTitle("Reply in thread")
	ui.ConfigureBox(m.threadComposer.Box, &m.cfg.Theme)
	m.inspector.SetDirection(flex.DirectionRow).
		AddItem(m.threadMessages, 0, 1, false).
		AddItem(m.threadComposer, 3, 0, true)
	m.main.SetDirection(flex.DirectionColumn).
		AddItem(m.sidebar, 0, m.cfg.Sidebar.WidthPercent, true).
		AddItem(m.right, 0, 100-m.cfg.Sidebar.WidthPercent, false)
	m.AddLayer(m.main, layers.WithName("main"), layers.WithResize(true), layers.WithVisible(true))
}

func orderSections(sections []slack.ChannelSection) []slack.ChannelSection {
	if len(sections) < 2 {
		return sections
	}
	byID := make(map[string]slack.ChannelSection, len(sections))
	referenced := make(map[string]bool, len(sections))
	for _, section := range sections {
		byID[section.ID] = section
		if section.NextChannelSectionID != "" {
			referenced[section.NextChannelSectionID] = true
		}
	}
	ordered := make([]slack.ChannelSection, 0, len(sections))
	seen := make(map[string]bool, len(sections))
	for _, candidate := range sections {
		if referenced[candidate.ID] {
			continue
		}
		for current := candidate; current.ID != "" && !seen[current.ID]; current = byID[current.NextChannelSectionID] {
			ordered = append(ordered, current)
			seen[current.ID] = true
		}
	}
	for _, section := range sections {
		if !seen[section.ID] {
			ordered = append(ordered, section)
		}
	}
	return ordered
}

func (m *Model) Update(msg tview.Msg) tview.Cmd {
	switch msg := msg.(type) {
	case tview.InitMsg:
		return tview.Batch(tview.SetTitle("slacker"), m.connectRealtime())
	case tview.KeyMsg:
		if msg.Key() == tcell.KeyCtrlC {
			return tview.Quit()
		}
		if msg.Key() == tcell.KeyEscape && m.editing != nil {
			m.cancelEdit()
			return nil
		}
		if msg.Key() == tcell.KeyEscape && m.activeThread != "" {
			m.closeInspector()
			return nil
		}
		if reference, ok := m.selectedMessage(); ok {
			switch msg.Str() {
			case "e":
				if reference.message.User == m.selfUserID {
					m.beginEdit(reference)
				}
				return nil
			case "d":
				if reference.message.User == m.selfUserID {
					m.pendingAction, m.pendingMessage = "delete", &reference
					m.AddLayer(modal.NewModel().SetText("Delete this message? This cannot be undone.").AddButtons([]string{"Delete", "Cancel"}), layers.WithName("message-action"), layers.WithResize(true), layers.WithOverlay())
				}
				return nil
			case "r":
				m.pendingAction, m.pendingMessage = "react", &reference
				m.AddLayer(modal.NewModel().SetText("Add a reaction").AddButtons([]string{"thumbsup", "heart", "joy", "tada", "eyes", "Cancel"}), layers.WithName("message-action"), layers.WithResize(true), layers.WithOverlay())
				return nil
			case "-":
				own := ownReactionNames(reference.message.Reactions, m.selfUserID)
				if len(own) > 0 {
					m.pendingAction, m.pendingMessage = "unreact", &reference
					m.AddLayer(modal.NewModel().SetText("Remove one of your reactions").AddButtons(append(own, "Cancel")), layers.WithName("message-action"), layers.WithResize(true), layers.WithOverlay())
				}
				return nil
			}
		}
		if msg.Key() == tcell.KeyEnter && m.activeThread != "" && m.main.Focused() == 2 && m.inspector.Focused() == 1 {
			text := strings.TrimSpace(m.threadComposer.Text())
			if text != "" {
				m.threadComposer.SetDisabled(true)
				if m.editing != nil && m.editingInThread {
					m.threadComposer.SetTitle("Saving edit…")
					return m.updateMessage(*m.editing, text)
				}
				return m.sendMessage(m.conversations[m.activeConversation], text, m.activeThread)
			}
		}
		if msg.Key() == tcell.KeyEnter && m.activeConversation != "" && m.main.Focused() == 1 && m.right.Focused() == 1 {
			text := strings.TrimSpace(m.composer.Text())
			if text != "" {
				conversation := m.conversations[m.activeConversation]
				m.composer.SetDisabled(true)
				if m.editing != nil {
					m.composer.SetTitle("Saving edit…")
					return m.updateMessage(*m.editing, text)
				}
				m.composer.SetTitle("Sending…")
				return m.sendMessage(conversation, text, m.activeThread)
			}
		}
	case tree.SelectedMsg:
		switch reference := msg.Node.Reference().(type) {
		case string:
			if reference == "" || m.client == nil {
				break
			}
			m.closeInspector()
			conversation := m.conversations[reference]
			m.activeConversation, m.activeThread = reference, ""
			m.messages.SetTitle(conversationTitle(conversation))
			m.setMessageNotice("Loading messages…")
			m.composer.SetDisabled(true)
			return m.loadHistory(conversation)
		case messageReference:
			timestamp := reference.message.Timestamp
			if reference.message.ThreadTS != "" {
				timestamp = reference.message.ThreadTS
			}
			return m.loadThread(reference.conversation, timestamp)
		case profileReference:
			if user, ok := m.users[reference.userID]; ok {
				profile := modal.NewModel().SetText(profileText(user)).AddButtons([]string{"Close"})
				m.AddLayer(profile, layers.WithName("profile"), layers.WithResize(true), layers.WithOverlay())
			}
		case threadBackReference:
			m.closeInspector()
			return nil
		}
	case historyLoadedMsg:
		if msg.conversation.ID != m.activeConversation {
			break
		}
		if msg.err != nil {
			m.setMessageNotice("Could not load messages: " + msg.err.Error())
			break
		}
		m.activeThread = ""
		m.showMessages(msg.conversation, msg.messages, msg.hasMore, false)
		m.composer.SetDisabled(false)
		m.composer.SetTitle("Message " + conversationTitle(msg.conversation))
	case messageSentMsg:
		if msg.conversation.ID != m.activeConversation {
			break
		}
		m.composer.SetDisabled(false)
		m.composer.SetTitle("Message " + conversationTitle(msg.conversation))
		if msg.err != nil {
			m.composer.SetTitle("Send failed: " + msg.err.Error())
			break
		}
		if msg.thread != "" {
			m.threadComposer.SetText("", false)
			m.threadComposer.SetDisabled(false)
		} else {
			m.composer.SetText("", false)
		}
		if msg.thread != "" {
			return m.loadThread(msg.conversation, msg.thread)
		}
		return m.loadHistory(msg.conversation)
	case messageMutatedMsg:
		m.composer.SetDisabled(false)
		m.threadComposer.SetDisabled(false)
		if msg.err != nil {
			if m.editingInThread {
				m.threadComposer.SetTitle(msg.action + " failed: " + msg.err.Error())
			} else {
				m.composer.SetTitle(msg.action + " failed: " + msg.err.Error())
			}
			break
		}
		m.editing = nil
		m.editingInThread = false
		m.composer.SetText("", false)
		m.threadComposer.SetText("", false)
		m.composer.SetTitle("Message " + conversationTitle(msg.conversation))
		m.threadComposer.SetTitle("Reply in thread")
		if msg.thread != "" {
			return m.loadThread(msg.conversation, msg.thread)
		}
		if msg.action == "Delete" && m.inspectorVisible {
			m.closeInspector()
		}
		return m.loadHistory(msg.conversation)
	case threadLoadedMsg:
		if msg.conversation.ID != m.activeConversation {
			break
		}
		if msg.err != nil {
			m.setMessageNotice("Could not load thread: " + msg.err.Error())
			break
		}
		m.activeThread = msg.timestamp
		m.showThread(msg.conversation, msg.messages)
		m.openInspector()
	case realtimeConnectedMsg:
		if msg.err != nil {
			return m.reconnectRealtime()
		}
		m.stream = msg.stream
		return m.nextRealtime()
	case realtimeEventMsg:
		if msg.err != nil {
			return m.reconnectRealtime()
		}
		commands := []tview.Cmd{m.nextRealtime()}
		if msg.event.ConversationID() == m.activeConversation && (msg.event.Type == "message" || msg.event.Type == "reaction_added" || msg.event.Type == "reaction_removed") {
			if m.activeThread != "" {
				commands = append(commands, m.loadThread(m.conversations[m.activeConversation], m.activeThread))
			} else {
				commands = append(commands, m.loadHistory(m.conversations[m.activeConversation]))
			}
		}
		return tview.Batch(commands...)
	case modal.DoneMsg:
		if m.pendingAction != "" {
			m.RemoveLayer("message-action")
			action, reference := m.pendingAction, m.pendingMessage
			m.pendingAction, m.pendingMessage = "", nil
			if reference == nil || msg.ButtonIndex < 0 || msg.ButtonLabel == "Cancel" {
				return nil
			}
			switch action {
			case "delete":
				if msg.ButtonLabel == "Delete" {
					return m.deleteMessage(*reference)
				}
			case "react":
				return m.setReaction(*reference, msg.ButtonLabel, true)
			case "unreact":
				return m.setReaction(*reference, msg.ButtonLabel, false)
			}
			return nil
		}
		m.RemoveLayer("profile")
		return nil
	}
	return m.Layers.Update(msg)
}

func (m *Model) selectedMessage() (messageReference, bool) {
	var node *tree.Node
	if m.main.Focused() == 1 && m.right.Focused() == 0 {
		node = m.messages.CurrentNode()
	} else if m.inspectorVisible && m.main.Focused() == 2 && m.inspector.Focused() == 0 {
		node = m.threadMessages.CurrentNode()
	}
	if node == nil {
		return messageReference{}, false
	}
	reference, ok := node.Reference().(messageReference)
	return reference, ok
}

func (m *Model) beginEdit(reference messageReference) {
	m.editing = &reference
	m.editingInThread = m.inspectorVisible && m.main.Focused() == 2
	if m.editingInThread {
		m.threadComposer.SetText(reference.message.Text, true)
		m.threadComposer.SetTitle("Edit message — Enter to save")
		m.inspector.SetFocus(1)
		return
	}
	m.composer.SetText(reference.message.Text, true)
	m.composer.SetTitle("Edit message — Enter to save")
	m.right.SetFocus(1)
}

func (m *Model) cancelEdit() {
	m.editing = nil
	m.composer.SetText("", false)
	m.threadComposer.SetText("", false)
	if conversation, ok := m.conversations[m.activeConversation]; ok {
		m.composer.SetTitle("Message " + conversationTitle(conversation))
	}
	m.threadComposer.SetTitle("Reply in thread")
	m.editingInThread = false
}

func (m *Model) updateMessage(reference messageReference, text string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := m.client.UpdateMessage(ctx, reference.conversation.ID, reference.message.Timestamp, text)
		return messageMutatedMsg{conversation: reference.conversation, thread: reference.contextThread, action: "Edit", err: err}
	}
}

func (m *Model) deleteMessage(reference messageReference) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.client.DeleteMessage(ctx, reference.conversation.ID, reference.message.Timestamp)
		thread := reference.contextThread
		if reference.message.Timestamp == thread {
			thread = ""
		}
		return messageMutatedMsg{conversation: reference.conversation, thread: thread, action: "Delete", err: err}
	}
}

func (m *Model) setReaction(reference messageReference, name string, add bool) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var err error
		if add {
			err = m.client.AddReaction(ctx, reference.conversation.ID, reference.message.Timestamp, name)
		} else {
			err = m.client.RemoveReaction(ctx, reference.conversation.ID, reference.message.Timestamp, name)
		}
		return messageMutatedMsg{conversation: reference.conversation, thread: reference.contextThread, action: "Reaction", err: err}
	}
}

func ownReactionNames(reactions []slack.Reaction, userID string) []string {
	var names []string
	for _, reaction := range reactions {
		for _, id := range reaction.Users {
			if id == userID {
				names = append(names, reaction.Name)
				break
			}
		}
	}
	return names
}

func (m *Model) connectRealtime() tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stream, err := m.client.OpenRealtime(ctx)
		return realtimeConnectedMsg{stream: stream, err: err}
	}
}

func (m *Model) nextRealtime() tview.Cmd {
	return func() tview.Msg {
		if m.stream == nil {
			return realtimeEventMsg{err: fmt.Errorf("realtime stream is not connected")}
		}
		event, err := m.stream.Next()
		return realtimeEventMsg{event: event, err: err}
	}
}

func (m *Model) reconnectRealtime() tview.Cmd {
	if m.stream != nil {
		_ = m.stream.Close()
		m.stream = nil
	}
	m.realtimeFailures++
	delay := time.Duration(1<<min(m.realtimeFailures, 6)) * time.Second
	return func() tview.Msg {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stream, err := m.client.OpenRealtime(ctx)
		return realtimeConnectedMsg{stream: stream, err: err}
	}
}

func (m *Model) sendMessage(conversation slack.Conversation, text, thread string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := m.client.PostMessage(ctx, conversation.ID, text, thread)
		return messageSentMsg{conversation: conversation, thread: thread, err: err}
	}
}

func (m *Model) loadThread(conversation slack.Conversation, timestamp string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		messages, _, err := m.client.ConversationReplies(ctx, conversation.ID, timestamp, 100)
		return threadLoadedMsg{conversation: conversation, timestamp: timestamp, messages: messages, err: err}
	}
}

func (m *Model) setMessageNotice(value string) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode(value).SetSelectable(false))
	m.messages.SetRoot(root).SetTopLevel(1)
}

func (m *Model) showMessages(conversation slack.Conversation, messages []slack.Message, hasMore, thread bool) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode("Enter thread/profile · r react · - remove reaction · e edit · d delete").SetSelectable(false))
	if thread {
		root.AddChild(tree.NewNode("← Back to channel").SetReference(threadBackReference{conversation: conversation}))
	}
	if hasMore {
		root.AddChild(tree.NewNode("— showing the latest messages —").SetSelectable(false))
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		author := message.Username
		if author == "" {
			author = message.User
		}
		if author == "" {
			author = "Slack"
		}
		if user, ok := m.users[message.User]; ok {
			author = displayName(user)
		}
		text := strings.ReplaceAll(m.renderSlackText(message.Text), "\n", " ↵ ")
		line := fmt.Sprintf("%s  %s  %s", author, formatTimestamp(message.Timestamp), text)
		if message.Edited != nil {
			line += "  (edited)"
		}
		if message.ReplyCount > 0 {
			line += fmt.Sprintf("  [%d replies]", message.ReplyCount)
		}
		node := tree.NewNode(line).SetReference(messageReference{conversation: conversation, message: message}).SetExpanded(true)
		if message.User != "" {
			node.AddChild(tree.NewNode("Profile: " + author).SetReference(profileReference{userID: message.User}))
		}
		for _, file := range message.Files {
			name := file.Title
			if name == "" {
				name = file.Name
			}
			node.AddChild(tree.NewNode("📎 " + name).SetSelectable(false))
		}
		if len(message.Reactions) > 0 {
			node.AddChild(tree.NewNode("Reactions: " + renderReactions(message.Reactions, m.selfUserID)).SetSelectable(false))
		}
		root.AddChild(node)
	}
	if len(messages) == 0 {
		root.AddChild(tree.NewNode("No messages yet.").SetSelectable(false))
	}
	m.messages.SetRoot(root).SetTopLevel(1)
	if thread {
		m.messages.SetTitle("Thread in " + conversationTitle(conversation))
	} else {
		m.messages.SetTitle(conversationTitle(conversation))
	}
}

func (m *Model) showThread(conversation slack.Conversation, messages []slack.Message) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode("× Close thread").SetReference(threadBackReference{conversation: conversation}))
	root.AddChild(tree.NewNode("r react · - remove reaction · e edit · d delete").SetSelectable(false))
	for _, message := range messages {
		author := message.Username
		if user, ok := m.users[message.User]; ok {
			author = displayName(user)
		}
		if author == "" {
			author = message.User
		}
		if author == "" {
			author = "Slack"
		}
		line := fmt.Sprintf("%s  %s  %s", author, formatTimestamp(message.Timestamp), strings.ReplaceAll(m.renderSlackText(message.Text), "\n", " ↵ "))
		if message.Edited != nil {
			line += "  (edited)"
		}
		node := tree.NewNode(line).SetReference(messageReference{conversation: conversation, message: message, contextThread: m.activeThread}).SetExpanded(true)
		if message.User != "" {
			node.AddChild(tree.NewNode("Profile: " + author).SetReference(profileReference{userID: message.User}))
		}
		if len(message.Reactions) > 0 {
			node.AddChild(tree.NewNode("Reactions: " + renderReactions(message.Reactions, m.selfUserID)).SetSelectable(false))
		}
		root.AddChild(node)
	}
	m.threadMessages.SetRoot(root).SetTopLevel(1).SetTitle("Thread in " + conversationTitle(conversation))
}

var slackEntityPattern = regexp.MustCompile(`<([@#!]?)([^>|]+)(?:\|([^>]+))?>`)
var slackFormatPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\*([^*\n]+)\*`),
	regexp.MustCompile(`_([^_\n]+)_`),
	regexp.MustCompile(`~([^~\n]+)~`),
	regexp.MustCompile("`([^`\\n]+)`"),
}

func (m *Model) renderSlackText(value string) string {
	value = slackEntityPattern.ReplaceAllStringFunc(value, func(entity string) string {
		parts := slackEntityPattern.FindStringSubmatch(entity)
		kind, id, label := parts[1], parts[2], parts[3]
		switch kind {
		case "@":
			if user, ok := m.users[id]; ok {
				return "@" + displayName(user)
			}
			return "@" + id
		case "#":
			if conversation, ok := m.conversations[id]; ok && conversation.Name != "" {
				return "#" + conversation.Name
			}
			if label != "" {
				return "#" + label
			}
			return "#" + id
		case "!":
			if label != "" {
				return "@" + label
			}
			return "@" + id
		default:
			if label != "" {
				return label + " (" + id + ")"
			}
			return id
		}
	})
	value = html.UnescapeString(value)
	for _, pattern := range slackFormatPatterns {
		value = pattern.ReplaceAllString(value, "$1")
	}
	return value
}

func renderReactions(reactions []slack.Reaction, selfUserID string) string {
	parts := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		own := false
		for _, id := range reaction.Users {
			own = own || id == selfUserID
		}
		marker := ""
		if own {
			marker = "*"
		}
		parts = append(parts, fmt.Sprintf(":%s: %d%s", reaction.Name, reaction.Count, marker))
	}
	return strings.Join(parts, "  ")
}

func (m *Model) openInspector() {
	if m.inspectorVisible {
		return
	}
	m.main.ResizeItem(m.sidebar, 0, 24)
	m.main.ResizeItem(m.right, 0, 48)
	m.main.AddItem(m.inspector, 0, 28, false)
	m.inspectorVisible = true
}

func (m *Model) closeInspector() {
	if !m.inspectorVisible {
		return
	}
	m.main.RemoveItem(m.inspector)
	m.main.ResizeItem(m.sidebar, 0, m.cfg.Sidebar.WidthPercent)
	m.main.ResizeItem(m.right, 0, 100-m.cfg.Sidebar.WidthPercent)
	m.activeThread = ""
	m.inspectorVisible = false
}

func displayName(user slack.User) string {
	if user.Profile.DisplayName != "" {
		return user.Profile.DisplayName
	}
	if user.Profile.RealName != "" {
		return user.Profile.RealName
	}
	if user.RealName != "" {
		return user.RealName
	}
	return user.Name
}

func profileText(user slack.User) string {
	name := displayName(user)
	lines := []string{name}
	if user.Profile.Title != "" {
		lines = append(lines, user.Profile.Title)
	}
	if user.Profile.Pronouns != "" {
		lines = append(lines, "Pronouns: "+user.Profile.Pronouns)
	}
	if user.Profile.StatusEmoji != "" || user.Profile.StatusText != "" {
		lines = append(lines, strings.TrimSpace(user.Profile.StatusEmoji+" "+user.Profile.StatusText))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) loadHistory(conversation slack.Conversation) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		messages, hasMore, err := m.client.ConversationHistory(ctx, conversation.ID, 100)
		return historyLoadedMsg{conversation: conversation, messages: messages, hasMore: hasMore, err: err}
	}
}

func conversationTitle(conversation slack.Conversation) string {
	name := conversation.Name
	if name == "" {
		name = conversation.ID
	}
	if conversation.IsIM || conversation.IsMPIM {
		return "@" + name
	}
	return "#" + name
}

func renderMessages(messages []slack.Message, hasMore bool) string {
	var out strings.Builder
	if hasMore {
		out.WriteString("— showing the latest messages —\n\n")
	}
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		author := message.Username
		if author == "" {
			author = message.User
		}
		if author == "" {
			author = "Slack"
		}
		fmt.Fprintf(&out, "%s  %s\n%s\n", author, formatTimestamp(message.Timestamp), message.Text)
		for _, file := range message.Files {
			name := file.Title
			if name == "" {
				name = file.Name
			}
			fmt.Fprintf(&out, "  📎 %s", name)
			if file.Mimetype != "" {
				fmt.Fprintf(&out, " (%s)", file.Mimetype)
			}
			out.WriteByte('\n')
		}
		if len(message.Reactions) > 0 {
			out.WriteString("  ")
			for _, reaction := range message.Reactions {
				fmt.Fprintf(&out, ":%s: %d  ", reaction.Name, reaction.Count)
			}
			out.WriteByte('\n')
		}
		if message.ReplyCount > 0 {
			fmt.Fprintf(&out, "  ↳ %d replies\n", message.ReplyCount)
		}
		out.WriteByte('\n')
	}
	if len(messages) == 0 {
		return "No messages yet."
	}
	return strings.TrimRight(out.String(), "\n")
}

func formatTimestamp(value string) string {
	seconds, _, ok := strings.Cut(value, ".")
	if !ok {
		seconds = value
	}
	parsed, err := time.ParseDuration(seconds + "s")
	if err != nil {
		return value
	}
	return time.Unix(int64(parsed.Seconds()), 0).Local().Format("Jan 2 15:04")
}

var _ tview.Model = (*Model)(nil)
