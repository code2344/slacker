package root

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/flex"
	"github.com/ayn2op/tview/layers"
	"github.com/ayn2op/tview/modal"
	styledtext "github.com/ayn2op/tview/text"
	"github.com/ayn2op/tview/tree"
	"github.com/code2344/slacker/internal/config"
	"github.com/code2344/slacker/internal/slack"
	"github.com/code2344/slacker/internal/ui"
	"github.com/gdamore/tcell/v3"
	"github.com/kyokomi/emoji/v2"
)

// Model is the first Slack-backed shell of the three-pane interface.
// Message loading and the composer are wired in subsequent backend slices.
type Model struct {
	*layers.Layers
	cfg                *config.Config
	sidebar            *tree.Model
	directMessages     *tree.Node
	apps               *tree.Node
	homeSidebarRoot    *tree.Node
	dmSidebarRoot      *tree.Node
	workspaceName      string
	header             *tview.TextView
	messages           *tree.Model
	composer           *tview.TextArea
	controls           *tview.TextView
	main               *flex.Model
	right              *flex.Model
	inspector          *flex.Model
	threadMessages     *tree.Model
	threadComposer     *tview.TextArea
	profile            *tree.Model
	inspectorVisible   bool
	inspectorKind      string
	client             *slack.Client
	users              map[string]slack.User
	profileFields      map[string]slack.TeamProfileField
	userLookups        map[string]bool
	presences          map[string]slack.UserPresence
	dnd                map[string]slack.DNDInfo
	profileLookups     map[string]bool
	counts             map[string]slack.ConversationCount
	conversations      map[string]slack.Conversation
	activeConversation string
	activeThread       string
	currentMessages    []slack.Message
	currentHasMore     bool
	searchMode         bool
	selfUserID         string
	editing            *messageReference
	editingInThread    bool
	pendingAction      string
	pendingMessage     *messageReference
	stream             *slack.Realtime
	realtimeFailures   int
	imagePreviews      map[string]kittyPreview
	kittyGraphics      bool
}

type kittyPreview struct {
	imageID uint32
	columns int
	rows    int
	pngData []byte
}

const (
	mainSidebarIndex    = 0
	mainMessagesIndex   = 1
	mainInspectorIndex  = 2
	rightMessagesIndex  = 1
	rightComposerIndex  = 2
	threadMessagesIndex = 0
	threadComposerIndex = 1
)

type historyLoadedMsg struct {
	conversation slack.Conversation
	messages     []slack.Message
	previews     map[string]kittyPreview
	hasMore      bool
	err          error
}

type messageSentMsg struct {
	conversation slack.Conversation
	thread       string
	message      slack.Message
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
type messageProfileReference struct{ messageReference }
type threadReference struct{ messageReference }
type threadBackReference struct{ conversation slack.Conversation }
type inspectorCloseReference struct{}
type openDMReference struct{ userID string }
type sidebarViewReference string
type fileReference struct{ file slack.File }
type searchMessageReference struct{ match slack.MessageSearchMatch }
type threadLoadedMsg struct {
	conversation slack.Conversation
	timestamp    string
	messages     []slack.Message
	previews     map[string]kittyPreview
	err          error
}
type userLoadedMsg struct {
	userID string
	user   slack.User
	err    error
}
type authorsLoadedMsg struct {
	users   map[string]slack.User
	history *historyLoadedMsg
	thread  *threadLoadedMsg
}
type profileDetailsLoadedMsg struct {
	userID   string
	presence slack.UserPresence
	dnd      slack.DNDInfo
}
type searchLoadedMsg struct {
	query        string
	messages     []slack.MessageSearchMatch
	messageTotal int
	files        []slack.File
	fileTotal    int
	err          error
}
type fileOpenedMsg struct {
	path string
	err  error
}
type dmOpenedMsg struct {
	conversation slack.Conversation
	err          error
}
type kittyUploadedMsg struct{}

type realtimeConnectedMsg struct {
	stream *slack.Realtime
	err    error
}
type realtimeEventMsg struct {
	event slack.RealtimeEvent
	err   error
}

func NewModel(cfg *config.Config, workspaceName, selfUserID string, conversations []slack.Conversation, sections []slack.ChannelSection, users map[string]slack.User, profileFields []slack.TeamProfileField, counts map[string]slack.ConversationCount, client *slack.Client) *Model {
	m := &Model{
		Layers:         layers.New(),
		cfg:            cfg,
		sidebar:        tree.NewModel(),
		header:         tview.NewTextView(),
		messages:       tree.NewModel(),
		composer:       tview.NewTextArea(),
		controls:       tview.NewTextView(),
		main:           flex.NewModel(),
		right:          flex.NewModel(),
		inspector:      flex.NewModel(),
		threadMessages: tree.NewModel(),
		threadComposer: tview.NewTextArea(),
		profile:        tree.NewModel(),
		client:         client,
		selfUserID:     selfUserID,
		users:          users,
		profileFields:  make(map[string]slack.TeamProfileField, len(profileFields)),
		userLookups:    make(map[string]bool),
		presences:      make(map[string]slack.UserPresence),
		dnd:            make(map[string]slack.DNDInfo),
		profileLookups: make(map[string]bool),
		counts:         counts,
		conversations:  make(map[string]slack.Conversation, len(conversations)),
		imagePreviews:  make(map[string]kittyPreview),
	}
	for _, field := range profileFields {
		m.profileFields[field.ID] = field
	}
	m.build(workspaceName, conversations, sections)
	return m
}

func (m *Model) build(workspaceName string, conversations []slack.Conversation, sections []slack.ChannelSection) {
	m.workspaceName = workspaceName
	sections = orderSections(sections)
	root := tree.NewNode("")
	channels := tree.NewNode("Channels").SetExpanded(true).SetSelectable(false)
	dms := tree.NewNode("Direct messages").SetExpanded(true).SetSelectable(false)
	apps := tree.NewNode("Apps").SetExpanded(true).SetSelectable(false)
	m.directMessages = dms
	m.apps = apps
	root.AddChild(tree.NewNode("💬  DMs").SetReference(sidebarViewReference("dms")))
	root.AddChild(dms)
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
		root.AddChild(channels)
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
				if conversation.IsIM || conversation.IsMPIM {
					continue
				}
				addConversation(parent, conversation)
			}
		}
	}
	var remaining, direct []slack.Conversation
	for _, conversation := range conversations {
		if conversation.IsIM || conversation.IsMPIM {
			direct = append(direct, conversation)
		} else if !assigned[conversation.ID] {
			remaining = append(remaining, conversation)
		}
	}
	sort.Slice(remaining, func(i, j int) bool { return remaining[i].Name < remaining[j].Name })
	for _, conversation := range remaining {
		if len(sections) > 0 {
			if channels.Children() == nil {
				root.AddChild(channels)
			}
			addConversation(channels, conversation)
		} else {
			addConversation(channels, conversation)
		}
	}
	checkedActivity := false
	for _, conversation := range direct {
		checkedActivity = checkedActivity || conversation.ActivityChecked
	}
	if checkedActivity {
		direct = slices.DeleteFunc(direct, func(conversation slack.Conversation) bool {
			return !conversation.ActivityChecked || conversation.LatestMessageTS == ""
		})
	}
	sort.SliceStable(direct, func(i, j int) bool { return conversationActivity(direct[i]) > conversationActivity(direct[j]) })
	dmRoot := tree.NewNode("")
	dmRoot.AddChild(tree.NewNode("← Home").SetReference(sidebarViewReference("home")))
	dmRoot.AddChild(tree.NewNode("Recent direct messages").SetSelectable(false))
	humanCount, appCount := 0, 0
	for _, conversation := range direct {
		isApp := m.isAppConversation(conversation)
		if !isApp && humanCount < 16 {
			addConversation(dms, conversation)
			humanCount++
		}
		if isApp && appCount < 8 {
			addConversation(apps, conversation)
			appCount++
		}
		if !isApp && len(dmRoot.Children()) < 52 {
			addConversation(dmRoot, conversation)
		}
	}
	if len(apps.Children()) > 0 {
		root.AddChild(apps)
	}
	m.homeSidebarRoot, m.dmSidebarRoot = root, dmRoot

	m.sidebar.SetRoot(root).SetTopLevel(1).SetTitle(workspaceName)
	ui.ConfigureBox(m.sidebar.Box, &m.cfg.Theme)
	ui.FocusBox(m.sidebar.Box, &m.cfg.Theme)

	m.messages.SetRoot(tree.NewNode("").AddChild(tree.NewNode(fmt.Sprintf("Connected to %s — select a conversation to begin.", workspaceName)).SetSelectable(false))).SetTopLevel(1).SetTitle("Messages")
	ui.ConfigureBox(m.messages.Box, &m.cfg.Theme)
	m.header.SetText("Messages\nSelect a conversation to begin").SetScrollable(false).SetWrap(false)

	m.composer.SetPlaceholder(nil)
	m.composer.SetDisabled(true)
	m.composer.SetTitle("Composer")
	ui.ConfigureBox(m.composer.Box, &m.cfg.Theme)
	m.controls.SetText("  Enter send/open   t thread   r react   e edit   d delete   / search   Esc close").SetScrollable(false).SetWrap(false)

	m.right.SetDirection(flex.DirectionRow).
		AddItem(m.header, 2, 0, false).
		AddItem(m.messages, 0, 1, false).
		AddItem(m.composer, 3, 0, true).
		AddItem(m.controls, 1, 0, false)
	m.threadMessages.SetRoot(tree.NewNode("")).SetTopLevel(1).SetTitle("Thread")
	ui.ConfigureBox(m.threadMessages.Box, &m.cfg.Theme)
	m.threadComposer.SetTitle("Reply in thread")
	ui.ConfigureBox(m.threadComposer.Box, &m.cfg.Theme)
	m.profile.SetRoot(tree.NewNode("")).SetTopLevel(1).SetTitle("Profile")
	ui.ConfigureBox(m.profile.Box, &m.cfg.Theme)
	m.inspector.SetDirection(flex.DirectionRow)
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
		terminal := strings.ToLower(msg.TerminalName + " " + os.Getenv("TERM") + " " + os.Getenv("TERM_PROGRAM"))
		m.kittyGraphics = strings.Contains(terminal, "kitty") || strings.Contains(terminal, "ghostty") || strings.Contains(terminal, "wezterm")
		return tview.Batch(tview.SetTitle("slacker"), m.connectRealtime())
	case tview.KeyMsg:
		if msg.Key() == tcell.KeyCtrlC {
			return tview.Quit()
		}
		if msg.Key() == tcell.KeyEscape && m.searchMode {
			m.searchMode = false
			m.composer.SetText("", false)
			if conversation, ok := m.conversations[m.activeConversation]; ok {
				m.composer.SetTitle("Message " + m.conversationTitle(conversation))
			}
			return nil
		}
		if msg.Key() == tcell.KeyEscape && m.editing != nil {
			m.cancelEdit()
			return nil
		}
		if msg.Key() == tcell.KeyEscape && m.inspectorVisible {
			m.closeInspector()
			return nil
		}
		if reference, ok := m.selectedMessage(); ok {
			switch msg.Str() {
			case "t":
				timestamp := reference.message.Timestamp
				if reference.message.ThreadTS != "" {
					timestamp = reference.message.ThreadTS
				}
				return m.loadThread(reference.conversation, timestamp)
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
		if msg.Str() == "/" && m.main.Focused() == mainMessagesIndex && m.right.Focused() == rightMessagesIndex {
			m.searchMode = true
			m.composer.SetText("", false)
			m.composer.SetTitle("Search messages and files — Enter to search · Esc to cancel")
			m.right.SetFocus(rightComposerIndex)
			return nil
		}
		if msg.Key() == tcell.KeyEnter && m.activeThread != "" && m.main.Focused() == mainInspectorIndex && m.inspector.Focused() == threadComposerIndex {
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
		if msg.Key() == tcell.KeyEnter && m.activeConversation != "" && m.main.Focused() == mainMessagesIndex && m.right.Focused() == rightComposerIndex {
			text := strings.TrimSpace(m.composer.Text())
			if text != "" {
				if m.searchMode {
					m.searchMode = false
					m.composer.SetDisabled(true)
					m.composer.SetTitle("Searching…")
					return m.search(text)
				}
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
		case sidebarViewReference:
			switch reference {
			case "dms":
				m.sidebar.SetRoot(m.dmSidebarRoot).SetTitle("Direct messages")
			case "home":
				m.sidebar.SetRoot(m.homeSidebarRoot).SetTitle(m.workspaceName)
			}
			return nil
		case string:
			if reference == "" || m.client == nil {
				break
			}
			m.closeInspector()
			conversation := m.conversations[reference]
			m.activeConversation, m.activeThread = reference, ""
			m.showConversationHeader(conversation)
			m.messages.SetTitle("Messages")
			m.setMessageNotice("Loading messages…")
			m.composer.SetDisabled(true)
			return m.loadHistory(conversation)
		case messageProfileReference:
			return m.showProfile(reference.message.User)
		case openDMReference:
			m.setMessageNotice("Opening direct message…")
			return m.openDM(reference.userID)
		case messageReference:
			return nil
		case threadReference:
			timestamp := reference.message.Timestamp
			if reference.message.ThreadTS != "" {
				timestamp = reference.message.ThreadTS
			}
			return m.loadThread(reference.conversation, timestamp)
		case threadBackReference:
			m.closeInspector()
			return nil
		case inspectorCloseReference:
			m.closeInspector()
			return nil
		case fileReference:
			m.messages.SetTitle("Opening " + fileName(reference.file) + "…")
			return m.downloadAndOpen(reference.file)
		case searchMessageReference:
			conversation := reference.match.Channel
			if known, ok := m.conversations[conversation.ID]; ok {
				conversation = known
			}
			m.activeConversation = conversation.ID
			m.showConversationHeader(conversation)
			m.closeInspector()
			m.setMessageNotice("Loading " + m.conversationTitle(conversation) + "…")
			return m.loadHistory(conversation)
		}
	case dmOpenedMsg:
		if msg.err != nil {
			m.setMessageNotice("Could not open direct message: " + msg.err.Error())
			break
		}
		m.conversations[msg.conversation.ID] = msg.conversation
		m.addDirectMessage(msg.conversation)
		m.activeConversation, m.activeThread = msg.conversation.ID, ""
		m.showConversationHeader(msg.conversation)
		m.closeInspector()
		m.messages.SetTitle("Messages")
		m.composer.SetDisabled(true)
		m.setMessageNotice("Loading messages…")
		return m.loadHistory(msg.conversation)
	case historyLoadedMsg:
		if msg.conversation.ID != m.activeConversation {
			break
		}
		if msg.err != nil {
			m.setMessageNotice("Could not load messages: " + msg.err.Error())
			break
		}
		for id, preview := range msg.previews {
			m.imagePreviews[id] = preview
		}
		if userIDs := m.unresolvedUserIDs(msg.messages); len(userIDs) > 0 {
			m.setMessageNotice("Resolving message authors…")
			return m.loadAuthors(userIDs, &msg, nil)
		}
		m.activeThread = ""
		m.currentMessages = append(m.currentMessages[:0], msg.messages...)
		m.currentHasMore = msg.hasMore
		m.showMessages(msg.conversation, msg.messages, msg.hasMore, false)
		m.composer.SetDisabled(false)
		m.composer.SetTitle("Message " + m.conversationTitle(msg.conversation))
		return uploadKittyPreviews(msg.previews)
	case messageSentMsg:
		if msg.conversation.ID != m.activeConversation {
			break
		}
		m.composer.SetDisabled(false)
		m.composer.SetTitle("Message " + m.conversationTitle(msg.conversation))
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
		message := msg.message
		if message.User == "" {
			message.User = m.selfUserID
		}
		m.currentMessages = append([]slack.Message{message}, m.currentMessages...)
		m.showMessages(msg.conversation, m.currentMessages, m.currentHasMore, false)
		return nil
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
		m.composer.SetTitle("Message " + m.conversationTitle(msg.conversation))
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
		for id, preview := range msg.previews {
			m.imagePreviews[id] = preview
		}
		if userIDs := m.unresolvedUserIDs(msg.messages); len(userIDs) > 0 {
			return m.loadAuthors(userIDs, nil, &msg)
		}
		m.activeThread = msg.timestamp
		m.showThread(msg.conversation, msg.messages)
		m.openInspector()
		return uploadKittyPreviews(msg.previews)
	case userLoadedMsg:
		if msg.err != nil {
			m.showProfileError(msg.userID, msg.err)
			break
		}
		if msg.user.ID == "" {
			msg.user.ID = msg.userID
		}
		m.users[msg.user.ID] = msg.user
		return m.showProfile(msg.user.ID)
	case authorsLoadedMsg:
		for id, user := range msg.users {
			if user.ID == "" {
				user.ID = id
			}
			m.users[id] = user
		}
		if msg.history != nil {
			return m.Update(*msg.history)
		}
		if msg.thread != nil {
			return m.Update(*msg.thread)
		}
	case profileDetailsLoadedMsg:
		m.presences[msg.userID] = msg.presence
		m.dnd[msg.userID] = msg.dnd
		return m.showProfile(msg.userID)
	case searchLoadedMsg:
		m.composer.SetDisabled(false)
		m.composer.SetText("", false)
		if msg.err != nil {
			m.composer.SetTitle("Search failed: " + msg.err.Error())
			break
		}
		m.showSearchResults(msg)
		m.composer.SetTitle("Search again with /")
	case fileOpenedMsg:
		if msg.err != nil {
			m.messages.SetTitle("File failed: " + msg.err.Error())
		} else {
			m.messages.SetTitle("Opened " + msg.path)
		}
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
		return nil
	}
	return m.Layers.Update(msg)
}

func (m *Model) selectedMessage() (messageReference, bool) {
	var node *tree.Node
	if m.main.Focused() == mainMessagesIndex && m.right.Focused() == rightMessagesIndex {
		node = m.messages.CurrentNode()
	} else if m.inspectorVisible && m.main.Focused() == mainInspectorIndex && m.inspector.Focused() == threadMessagesIndex {
		node = m.threadMessages.CurrentNode()
	}
	if node == nil {
		return messageReference{}, false
	}
	switch reference := node.Reference().(type) {
	case messageReference:
		return reference, true
	case messageProfileReference:
		return reference.messageReference, true
	case threadReference:
		return reference.messageReference, true
	default:
		return messageReference{}, false
	}
}

func (m *Model) beginEdit(reference messageReference) {
	m.editing = &reference
	m.editingInThread = m.inspectorVisible && m.main.Focused() == mainInspectorIndex
	if m.editingInThread {
		m.threadComposer.SetText(reference.message.Text, true)
		m.threadComposer.SetTitle("Edit message — Enter to save")
		m.inspector.SetFocus(threadComposerIndex)
		return
	}
	m.composer.SetText(reference.message.Text, true)
	m.composer.SetTitle("Edit message — Enter to save")
	m.right.SetFocus(rightComposerIndex)
}

func (m *Model) cancelEdit() {
	m.editing = nil
	m.composer.SetText("", false)
	m.threadComposer.SetText("", false)
	if conversation, ok := m.conversations[m.activeConversation]; ok {
		m.composer.SetTitle("Message " + m.conversationTitle(conversation))
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
		message, err := m.client.PostMessage(ctx, conversation.ID, text, thread)
		return messageSentMsg{conversation: conversation, thread: thread, message: message, err: err}
	}
}

func (m *Model) loadThread(conversation slack.Conversation, timestamp string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		messages, _, err := m.client.ConversationReplies(ctx, conversation.ID, timestamp, 100)
		previews := m.loadKittyPreviews(ctx, messages)
		return threadLoadedMsg{conversation: conversation, timestamp: timestamp, messages: messages, previews: previews, err: err}
	}
}

func (m *Model) setMessageNotice(value string) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode(value).SetSelectable(false))
	m.messages.SetRoot(root).SetTopLevel(1)
}

func (m *Model) showMessages(conversation slack.Conversation, messages []slack.Message, hasMore, thread bool) {
	root := tree.NewNode("")
	if thread {
		root.AddChild(tree.NewNode("← Back to channel").SetReference(threadBackReference{conversation: conversation}))
	}
	if hasMore {
		root.AddChild(tree.NewNode("— showing the latest messages —").SetSelectable(false))
	}
	previousAuthor := ""
	var previousTime time.Time
	var latestNode *tree.Node
	now := time.Now()
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
		messageTime := parseSlackTimestamp(message.Timestamp)
		authorKey := message.User
		if authorKey == "" {
			authorKey = message.Username
		}
		compact := previousAuthor != "" && authorKey == previousAuthor && !previousTime.IsZero() && !messageTime.IsZero() && messageTime.Sub(previousTime) <= 5*time.Minute
		text := prepareMrkdwn(m.resolveSlackEntities(message.Text))
		when := formatRelativeTimestamp(message.Timestamp, now)
		reference := messageReference{conversation: conversation, message: message}
		if compact {
			latestNode = tree.NewNode("").SetLine(renderMrkdwnLine(fmt.Sprintf("  %-14s  ", when), text, message.Edited != nil)).SetReference(reference).SetExpanded(true)
			root.AddChild(latestNode)
		} else {
			root.AddChild(tree.NewNode(fmt.Sprintf("@%s  %s", author, when)).SetReference(messageProfileReference{messageReference: reference}))
			latestNode = tree.NewNode("").SetLine(renderMrkdwnLine("  ", text, message.Edited != nil)).SetReference(reference).SetExpanded(true)
			root.AddChild(latestNode)
		}
		for _, file := range message.Files {
			name := file.Title
			if name == "" {
				name = file.Name
			}
			label := "file"
			if strings.HasPrefix(file.Mimetype, "image/") {
				label = "image"
			}
			root.AddChild(tree.NewNode("  [" + label + "] " + name + " — open").SetReference(fileReference{file: file}))
			if preview, ok := m.imagePreviews[file.ID]; ok {
				for _, line := range kittyPlaceholderLines(preview) {
					root.AddChild(tree.NewNode("").SetLine(line).SetSelectable(false))
				}
			}
		}
		if len(message.Reactions) > 0 {
			root.AddChild(tree.NewNode("  " + renderReactions(message.Reactions, m.selfUserID)).SetSelectable(false))
		}
		if message.ReplyCount > 0 {
			label := "reply"
			if message.ReplyCount != 1 {
				label = "replies"
			}
			root.AddChild(tree.NewNode(fmt.Sprintf("  [%d %s — open thread]", message.ReplyCount, label)).SetReference(threadReference{messageReference: reference}))
		}
		previousAuthor, previousTime = authorKey, messageTime
	}
	if len(messages) == 0 {
		root.AddChild(tree.NewNode("No messages yet.").SetSelectable(false))
	}
	m.messages.SetRoot(root).SetTopLevel(1)
	if latestNode != nil {
		m.messages.SetCurrentNode(latestNode)
	}
	m.messages.SetTitle("Messages")
}

func (m *Model) showThread(conversation slack.Conversation, messages []slack.Message) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode("× Close thread").SetReference(threadBackReference{conversation: conversation}))
	previousAuthor := ""
	var previousTime time.Time
	var latestNode *tree.Node
	now := time.Now()
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
		messageTime := parseSlackTimestamp(message.Timestamp)
		authorKey := message.User
		if authorKey == "" {
			authorKey = message.Username
		}
		compact := previousAuthor != "" && authorKey == previousAuthor && !previousTime.IsZero() && !messageTime.IsZero() && messageTime.Sub(previousTime) <= 5*time.Minute
		when := formatRelativeTimestamp(message.Timestamp, now)
		text := prepareMrkdwn(m.resolveSlackEntities(message.Text))
		reference := messageReference{conversation: conversation, message: message, contextThread: m.activeThread}
		if compact {
			latestNode = tree.NewNode("").SetLine(renderMrkdwnLine(fmt.Sprintf("  %-14s  ", when), text, message.Edited != nil)).SetReference(reference).SetExpanded(true)
			root.AddChild(latestNode)
		} else {
			root.AddChild(tree.NewNode(fmt.Sprintf("@%s  %s", author, when)).SetReference(messageProfileReference{messageReference: reference}))
			latestNode = tree.NewNode("").SetLine(renderMrkdwnLine("  ", text, message.Edited != nil)).SetReference(reference).SetExpanded(true)
			root.AddChild(latestNode)
		}
		for _, file := range message.Files {
			kind := "file"
			if strings.HasPrefix(file.Mimetype, "image/") {
				kind = "image"
			}
			root.AddChild(tree.NewNode("  [" + kind + "] " + fileName(file) + " — open").SetReference(fileReference{file: file}))
			if preview, ok := m.imagePreviews[file.ID]; ok {
				for _, line := range kittyPlaceholderLines(preview) {
					root.AddChild(tree.NewNode("").SetLine(line).SetSelectable(false))
				}
			}
		}
		if len(message.Reactions) > 0 {
			root.AddChild(tree.NewNode("  " + renderReactions(message.Reactions, m.selfUserID)).SetSelectable(false))
		}
		previousAuthor, previousTime = authorKey, messageTime
	}
	m.threadMessages.SetRoot(root).SetTopLevel(1).SetTitle("Thread in " + m.conversationTitle(conversation))
	if latestNode != nil {
		m.threadMessages.SetCurrentNode(latestNode)
	}
	m.showThreadInspector()
}

var slackEntityPattern = regexp.MustCompile(`<([@#!]?)([^>|]+)(?:\|([^>]+))?>`)
var emojiAliasPattern = regexp.MustCompile(`:[[:alnum:]_+\-]+:`)
var slackBoldPattern = regexp.MustCompile(`\*([^*\n]+)\*`)
var slackItalicPattern = regexp.MustCompile(`(^|[\s(])_([^_\n]+)_($|[\s).,!?:])`)
var slackStrikePattern = regexp.MustCompile(`~([^~\n]+)~`)
var slackCodePattern = regexp.MustCompile("`([^`\\n]+)`")

func (m *Model) renderSlackText(value string) string {
	value = m.resolveSlackEntities(value)
	value = slackBoldPattern.ReplaceAllString(value, "$1")
	value = slackItalicPattern.ReplaceAllString(value, "$1$2$3")
	value = slackStrikePattern.ReplaceAllString(value, "$1")
	value = slackCodePattern.ReplaceAllString(value, "$1")
	return value
}

func (m *Model) resolveSlackEntities(value string) string {
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
			if strings.HasPrefix(id, "date^") {
				if label != "" {
					return label
				}
				return "date"
			}
			if id == "channel" || id == "everyone" || id == "here" {
				return "@" + id
			}
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
	return renderEmojiAliases(html.UnescapeString(value))
}

func renderEmojiAliases(value string) string {
	codes := emoji.CodeMap()
	return emojiAliasPattern.ReplaceAllStringFunc(value, func(alias string) string {
		if rendered, ok := codes[alias]; ok {
			return strings.TrimSpace(rendered)
		}
		if strings.HasPrefix(alias, ":flag-") {
			return strings.TrimSpace(emoji.Emojize(alias))
		}
		return alias
	})
}

func renderMrkdwnLine(prefix, value string, edited bool) styledtext.Line {
	line := styledtext.Line{styledtext.NewSegment(prefix, tcell.StyleDefault)}
	line = append(line, parseMrkdwnSegments(value, tcell.StyleDefault)...)
	if edited {
		line = append(line, styledtext.NewSegment("  (edited)", tcell.StyleDefault.Italic(true)))
	}
	return line
}

func prepareMrkdwn(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, ">>>") {
			lines[index] = "│ " + strings.TrimSpace(strings.TrimPrefix(line, ">>>"))
			for following := index + 1; following < len(lines); following++ {
				lines[following] = "│ " + lines[following]
			}
			break
		}
		if strings.HasPrefix(line, ">") {
			lines[index] = "│ " + strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
	}
	return strings.Join(lines, " ↵ ")
}

func parseMrkdwnSegments(value string, style tcell.Style) styledtext.Line {
	var line styledtext.Line
	for len(value) > 0 {
		if strings.HasPrefix(value, "```") {
			end := strings.Index(value[3:], "```")
			if end < 0 {
				line = append(line, styledtext.NewSegment(value, style.Reverse(true)))
				break
			}
			end += 3
			line = append(line, styledtext.NewSegment(value[3:end], style.Reverse(true)))
			value = value[end+3:]
			continue
		}
		index := strings.IndexAny(value, "*_~`")
		if index < 0 {
			line = append(line, styledtext.NewSegment(value, style))
			break
		}
		if index > 0 {
			line = append(line, styledtext.NewSegment(value[:index], style))
			value = value[index:]
		}
		marker := value[0]
		end := strings.IndexByte(value[1:], marker)
		if end < 0 {
			line = append(line, styledtext.NewSegment(value[:1], style))
			value = value[1:]
			continue
		}
		end++
		inner := value[1:end]
		innerStyle := style
		switch marker {
		case '*':
			innerStyle = innerStyle.Bold(true)
		case '_':
			innerStyle = innerStyle.Italic(true)
		case '~':
			innerStyle = innerStyle.StrikeThrough(true)
		case '`':
			innerStyle = innerStyle.Reverse(true)
		}
		line = append(line, parseMrkdwnSegments(inner, innerStyle)...)
		value = value[end+1:]
	}
	return line
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
		parts = append(parts, fmt.Sprintf("%s %d%s", renderEmojiAliases(":"+reaction.Name+":"), reaction.Count, marker))
	}
	return strings.Join(parts, "  ")
}

func (m *Model) showProfile(userID string) tview.Cmd {
	user, ok := m.users[userID]
	if !ok || !user.ProfileLoaded {
		root := tree.NewNode("")
		root.AddChild(tree.NewNode("× Close profile").SetReference(inspectorCloseReference{}))
		root.AddChild(tree.NewNode("Loading profile…").SetSelectable(false))
		m.profile.SetRoot(root).SetTopLevel(1).SetTitle("Profile")
		m.activeThread = ""
		m.inspector.Clear().SetDirection(flex.DirectionRow).AddItem(m.profile, 0, 1, true)
		m.inspectorKind = "profile"
		m.openInspector()
		return m.loadUser(userID)
	}
	root := tree.NewNode("")
	root.AddChild(tree.NewNode("× Close profile").SetReference(inspectorCloseReference{}))
	realName := user.Profile.RealName
	if realName == "" {
		realName = user.RealName
	}
	if realName == "" {
		realName = displayName(user)
	}
	root.AddChild(tree.NewNode(realName).SetSelectable(false))
	handle := displayName(user)
	if handle != "" && handle != realName {
		root.AddChild(tree.NewNode("@" + handle).SetSelectable(false))
	} else if user.Name != "" && user.Name != realName {
		root.AddChild(tree.NewNode("@" + user.Name).SetSelectable(false))
	}
	dmReference := any(openDMReference{userID: userID})
	for _, conversation := range m.conversations {
		if conversation.IsIM && conversation.User == userID {
			dmReference = conversation.ID
			break
		}
	}
	root.AddChild(tree.NewNode("Message @" + handle).SetReference(dmReference))
	if user.Profile.Title != "" || user.Profile.Pronouns != "" {
		summary := user.Profile.Title
		if user.Profile.Pronouns != "" {
			if summary != "" {
				summary += "  ·  "
			}
			summary += user.Profile.Pronouns
		}
		root.AddChild(tree.NewNode(summary).SetSelectable(false))
	}
	if user.Profile.StatusEmoji != "" || user.Profile.StatusText != "" {
		root.AddChild(tree.NewNode("").SetSelectable(false))
		root.AddChild(tree.NewNode(strings.TrimSpace(user.Profile.StatusEmoji + " " + user.Profile.StatusText)).SetSelectable(false))
		if user.Profile.StatusExpiration > 0 {
			expires := time.Unix(user.Profile.StatusExpiration, 0).Local()
			root.AddChild(tree.NewNode("Until " + expires.Format("Mon Jan 2, 3:04 PM")).SetSelectable(false))
		}
	}
	if user.Profile.OOOMessage != "" {
		root.AddChild(tree.NewNode("Out of office  " + user.Profile.OOOMessage).SetSelectable(false))
	}
	if presence, exists := m.presences[userID]; exists && presence.Presence != "" {
		label := strings.Title(presence.Presence)
		if presence.ManualAway {
			label = "Away"
		}
		if presence.Presence == "away" && presence.LastActivity > 0 {
			label += "  ·  last active " + formatRelativeTimestamp(fmt.Sprintf("%d.0", presence.LastActivity), time.Now())
		}
		if dnd := m.dnd[userID]; dnd.SnoozeEnabled || dnd.DNDEnabled {
			label += "  ·  notifications snoozed"
			if dnd.SnoozeEndtime > 0 {
				label += " until " + time.Unix(dnd.SnoozeEndtime, 0).Local().Format("3:04 PM")
			}
		}
		root.AddChild(tree.NewNode(label).SetSelectable(false))
	}
	if user.TZOffset != 0 || user.TZLabel != "" {
		local := time.Now().UTC().Add(time.Duration(user.TZOffset) * time.Second).Format("3:04 PM")
		label := "Local time  " + local
		if user.TZLabel != "" {
			label += "  ·  " + user.TZLabel
		}
		root.AddChild(tree.NewNode(label).SetSelectable(false))
	}

	contact := [][2]string{}
	if user.Profile.Email != "" {
		contact = append(contact, [2]string{"Email", user.Profile.Email})
	}
	if user.Profile.Phone != "" {
		contact = append(contact, [2]string{"Phone", user.Profile.Phone})
	}
	if user.Profile.Skype != "" {
		contact = append(contact, [2]string{"Skype", user.Profile.Skype})
	}
	addProfileSection(root, "Contact information", contact)

	about := [][2]string{}
	if user.Profile.StartDate != "" {
		about = append(about, [2]string{"Start date", user.Profile.StartDate})
	}
	fields := make([]slack.TeamProfileField, 0, len(user.Profile.Fields))
	for id := range user.Profile.Fields {
		definition, exists := m.profileFields[id]
		if !exists {
			definition = slack.TeamProfileField{ID: id, Label: id, Ordering: 1 << 30}
		}
		fields = append(fields, definition)
	}
	sort.SliceStable(fields, func(i, j int) bool {
		if fields[i].Ordering == fields[j].Ordering {
			return fields[i].Label < fields[j].Label
		}
		return fields[i].Ordering < fields[j].Ordering
	})
	for _, definition := range fields {
		field := user.Profile.Fields[definition.ID]
		value := field.Value
		if value == "" {
			value = field.Alt
		}
		if value == "" {
			continue
		}
		label := definition.Label
		if label == "" {
			label = definition.FieldName
		}
		about = append(about, [2]string{label, m.renderSlackText(value)})
	}
	addProfileSection(root, "About", about)

	account := [][2]string{}
	roles := profileRoles(user)
	if len(roles) > 0 {
		account = append(account, [2]string{"Role", strings.Join(roles, ", ")})
	}
	if user.TZ != "" {
		account = append(account, [2]string{"Timezone", user.TZ})
	}
	if user.ID != "" {
		account = append(account, [2]string{"Member ID", user.ID})
	}
	addProfileSection(root, "Account", account)
	m.profile.SetRoot(root).SetTopLevel(1).SetTitle("Profile")
	m.activeThread = ""
	m.inspector.Clear().SetDirection(flex.DirectionRow).AddItem(m.profile, 0, 1, true)
	m.inspectorKind = "profile"
	m.openInspector()
	if !m.profileLookups[userID] {
		m.profileLookups[userID] = true
		return m.loadProfileDetails(userID)
	}
	return nil
}

func addProfileSection(root *tree.Node, heading string, rows [][2]string) {
	if len(rows) == 0 {
		return
	}
	root.AddChild(tree.NewNode("").SetSelectable(false))
	root.AddChild(tree.NewNode(heading).SetSelectable(false))
	for _, row := range rows {
		root.AddChild(tree.NewNode(row[0] + "  " + row[1]).SetSelectable(false))
	}
}

func profileRoles(user slack.User) []string {
	var roles []string
	switch {
	case user.IsPrimaryOwner:
		roles = append(roles, "Primary owner")
	case user.IsOwner:
		roles = append(roles, "Owner")
	case user.IsAdmin:
		roles = append(roles, "Admin")
	}
	if user.IsUltraRestricted {
		roles = append(roles, "Single-channel guest")
	} else if user.IsRestricted {
		roles = append(roles, "Guest")
	}
	if user.IsBot {
		roles = append(roles, "Bot")
	}
	return roles
}

func (m *Model) showProfileError(userID string, err error) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode("× Close profile").SetReference(inspectorCloseReference{}))
	root.AddChild(tree.NewNode("Could not load profile for " + userID + ": " + err.Error()).SetSelectable(false))
	m.profile.SetRoot(root).SetTopLevel(1).SetTitle("Profile")
}

func (m *Model) loadUser(userID string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		user, err := m.client.UserInfo(ctx, userID)
		return userLoadedMsg{userID: userID, user: user, err: err}
	}
}

func (m *Model) search(query string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var messages []slack.MessageSearchMatch
		var files []slack.File
		var messageTotal, fileTotal int
		var messageErr, fileErr error
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			messages, messageTotal, messageErr = m.client.SearchMessages(ctx, query)
		}()
		go func() {
			defer group.Done()
			files, fileTotal, fileErr = m.client.SearchFiles(ctx, query)
		}()
		group.Wait()
		var err error
		if messageErr != nil && fileErr != nil {
			err = fmt.Errorf("messages: %v; files: %v", messageErr, fileErr)
		}
		return searchLoadedMsg{query: query, messages: messages, messageTotal: messageTotal, files: files, fileTotal: fileTotal, err: err}
	}
}

func (m *Model) showSearchResults(result searchLoadedMsg) {
	root := tree.NewNode("")
	root.AddChild(tree.NewNode(fmt.Sprintf("Messages  %d matches", result.messageTotal)).SetSelectable(false))
	for _, match := range result.messages {
		author := match.Username
		if user, ok := m.users[match.User]; ok {
			author = displayName(user)
		}
		if author == "" {
			author = match.User
		}
		channel := match.Channel.Name
		if channel == "" {
			channel = match.Channel.ID
		}
		line := fmt.Sprintf("#%s  @%s  %s  %s", channel, author, formatRelativeTimestamp(match.Timestamp, time.Now()), strings.ReplaceAll(m.renderSlackText(match.Text), "\n", " ↵ "))
		root.AddChild(tree.NewNode(line).SetReference(searchMessageReference{match: match}))
	}
	root.AddChild(tree.NewNode("").SetSelectable(false))
	root.AddChild(tree.NewNode(fmt.Sprintf("Files  %d matches", result.fileTotal)).SetSelectable(false))
	for _, file := range result.files {
		kind := file.PrettyType
		if kind == "" {
			kind = file.Mimetype
		}
		root.AddChild(tree.NewNode(fmt.Sprintf("[%s] %s  %s — open", kind, fileName(file), formatBytes(file.Size))).SetReference(fileReference{file: file}))
	}
	if len(result.messages) == 0 && len(result.files) == 0 {
		root.AddChild(tree.NewNode("No results.").SetSelectable(false))
	}
	m.messages.SetRoot(root).SetTopLevel(1).SetTitle("Search: " + result.query)
	m.right.SetFocus(rightMessagesIndex)
}

func (m *Model) downloadAndOpen(file slack.File) tview.Cmd {
	return func() tview.Msg {
		url := file.URLPrivateDownload
		if url == "" {
			url = file.URLPrivate
		}
		if url == "" {
			return fileOpenedMsg{err: fmt.Errorf("Slack returned no downloadable URL")}
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return fileOpenedMsg{err: err}
		}
		directory := filepath.Join(home, "Downloads", "Slacker")
		if err := os.MkdirAll(directory, 0700); err != nil {
			return fileOpenedMsg{err: err}
		}
		name := filepath.Base(fileName(file))
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = file.ID
		}
		target := filepath.Join(directory, name)
		if _, err := os.Stat(target); err == nil {
			target = filepath.Join(directory, file.ID+"-"+name)
		}
		temporary, err := os.CreateTemp(directory, ".slacker-download-*")
		if err != nil {
			return fileOpenedMsg{err: err}
		}
		temporaryName := temporary.Name()
		defer os.Remove(temporaryName)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := m.client.Download(ctx, url, temporary); err != nil {
			temporary.Close()
			return fileOpenedMsg{err: err}
		}
		if err := temporary.Close(); err != nil {
			return fileOpenedMsg{err: err}
		}
		if err := os.Rename(temporaryName, target); err != nil {
			return fileOpenedMsg{err: err}
		}
		if err := openLocalFile(target); err != nil {
			return fileOpenedMsg{path: target, err: err}
		}
		return fileOpenedMsg{path: target}
	}
}

func fileName(file slack.File) string {
	if file.Title != "" {
		return file.Title
	}
	if file.Name != "" {
		return file.Name
	}
	return file.ID
}

func formatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}

func openLocalFile(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", path)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	return command.Start()
}

func kittyImagePayload(pngData []byte, imageID uint32, columns, rows int) string {
	payload := base64.StdEncoding.EncodeToString(pngData)
	var output bytes.Buffer
	for first := true; len(payload) > 0; first = false {
		chunkSize := min(4096, len(payload))
		chunk, rest := payload[:chunkSize], payload[chunkSize:]
		more := 0
		if len(rest) > 0 {
			more = 1
		}
		if first {
			fmt.Fprintf(&output, "\x1b_Ga=T,f=100,q=2,U=1,C=1,i=%d,c=%d,r=%d,m=%d;%s\x1b\\", imageID, columns, rows, more, chunk)
		} else {
			fmt.Fprintf(&output, "\x1b_Gq=2,m=%d;%s\x1b\\", more, chunk)
		}
		payload = rest
	}
	return output.String()
}

var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
}

func kittyPlaceholderLines(preview kittyPreview) []styledtext.Line {
	red := int32((preview.imageID >> 16) & 0xff)
	green := int32((preview.imageID >> 8) & 0xff)
	blue := int32(preview.imageID & 0xff)
	style := tcell.StyleDefault.Foreground(tcell.NewRGBColor(red, green, blue))
	lines := make([]styledtext.Line, preview.rows)
	for row := range preview.rows {
		line := styledtext.Line{styledtext.NewSegment("  ", tcell.StyleDefault)}
		for column := range preview.columns {
			cell := string([]rune{0x10EEEE, kittyDiacritics[row], kittyDiacritics[column]})
			line = append(line, styledtext.NewSegment(cell, style))
		}
		lines[row] = line
	}
	return lines
}

func uploadKittyPreviews(previews map[string]kittyPreview) tview.Cmd {
	if len(previews) == 0 {
		return nil
	}
	ids := make([]string, 0, len(previews))
	for id := range previews {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	commands := make([]tview.Cmd, 0, len(ids)+1)
	for _, id := range ids {
		preview := previews[id]
		commands = append(commands, tview.Raw(kittyImagePayload(preview.pngData, preview.imageID, preview.columns, preview.rows)))
	}
	commands = append(commands, func() tview.Msg { return kittyUploadedMsg{} })
	return tview.Sequence(commands...)
}

func (m *Model) loadKittyPreviews(ctx context.Context, messages []slack.Message) map[string]kittyPreview {
	previews := make(map[string]kittyPreview)
	if !m.kittyGraphics {
		return previews
	}
	for _, message := range messages {
		for _, file := range message.Files {
			if len(previews) >= 4 || file.ID == "" || !strings.HasPrefix(file.Mimetype, "image/") || file.Size > 12*1024*1024 {
				continue
			}
			target := file.URLPrivateDownload
			if target == "" {
				target = file.URLPrivate
			}
			if target == "" {
				continue
			}
			cacheRoot, _ := os.UserCacheDir()
			cachePath := ""
			var encodedData []byte
			if cacheRoot != "" {
				cachePath = filepath.Join(cacheRoot, "slacker", "images", filepath.Base(file.ID)+".png")
				encodedData, _ = os.ReadFile(cachePath)
			}
			if len(encodedData) == 0 {
				var downloaded bytes.Buffer
				if err := m.client.Download(ctx, target, &downloaded); err != nil || downloaded.Len() > 12*1024*1024 {
					continue
				}
				img, _, err := image.Decode(bytes.NewReader(downloaded.Bytes()))
				if err != nil {
					continue
				}
				var encoded bytes.Buffer
				if png.Encode(&encoded, img) != nil {
					continue
				}
				encodedData = encoded.Bytes()
				if cachePath != "" && os.MkdirAll(filepath.Dir(cachePath), 0700) == nil {
					_ = os.WriteFile(cachePath, encodedData, 0600)
				}
			}
			img, _, err := image.Decode(bytes.NewReader(encodedData))
			if err != nil {
				continue
			}
			bounds := img.Bounds()
			if bounds.Dx() < 1 || bounds.Dy() < 1 {
				continue
			}
			columns := min(40, max(8, bounds.Dx()/20))
			rows := min(16, max(3, columns*bounds.Dy()/bounds.Dx()/2))
			hash := fnv.New32a()
			_, _ = hash.Write([]byte(file.ID))
			imageID := hash.Sum32() & 0xFFFFFF
			if imageID == 0 {
				imageID = 1
			}
			previews[file.ID] = kittyPreview{imageID: imageID, columns: columns, rows: rows, pngData: encodedData}
		}
	}
	return previews
}

func (m *Model) loadProfileDetails(userID string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var presence slack.UserPresence
		var dnd slack.DNDInfo
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			presence, _ = m.client.UserPresence(ctx, userID)
		}()
		go func() {
			defer group.Done()
			dnd, _ = m.client.DNDInfo(ctx, userID)
		}()
		group.Wait()
		return profileDetailsLoadedMsg{userID: userID, presence: presence, dnd: dnd}
	}
}

func (m *Model) unresolvedUserIDs(messages []slack.Message) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, message := range messages {
		id := message.User
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if user, exists := m.users[id]; (exists && user.ProfileLoaded) || m.userLookups[id] {
			continue
		}
		m.userLookups[id] = true
		ids = append(ids, id)
	}
	return ids
}

func (m *Model) loadAuthors(userIDs []string, history *historyLoadedMsg, thread *threadLoadedMsg) tview.Cmd {
	return func() tview.Msg {
		users := make(map[string]slack.User, len(userIDs))
		var mu sync.Mutex
		semaphore := make(chan struct{}, 6)
		var group sync.WaitGroup
		for _, id := range userIDs {
			group.Add(1)
			go func(userID string) {
				defer group.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				user, err := m.client.UserInfo(ctx, userID)
				if err != nil {
					return
				}
				mu.Lock()
				users[userID] = user
				mu.Unlock()
			}(id)
		}
		group.Wait()
		return authorsLoadedMsg{users: users, history: history, thread: thread}
	}
}

func (m *Model) showThreadInspector() {
	m.inspector.Clear().SetDirection(flex.DirectionRow).
		AddItem(m.threadMessages, 0, 1, false).
		AddItem(m.threadComposer, 3, 0, true)
	m.inspectorKind = "thread"
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
	m.inspectorKind = ""
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

func parseSlackTimestamp(value string) time.Time {
	seconds, _, _ := strings.Cut(value, ".")
	parsed, err := time.ParseDuration(seconds + "s")
	if err != nil {
		return time.Time{}
	}
	return time.Unix(int64(parsed.Seconds()), 0).Local()
}

func formatRelativeTimestamp(value string, now time.Time) string {
	when := parseSlackTimestamp(value)
	if when.IsZero() {
		return value
	}
	age := now.Sub(when)
	if age < 0 || age >= 7*24*time.Hour {
		return when.Format("Jan 2 15:04")
	}
	if age < time.Minute {
		return "just now"
	}
	if age < time.Hour {
		minutes := int(age / time.Minute)
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if age < 24*time.Hour {
		hours := int(age / time.Hour)
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	return when.Format("Mon 15:04")
}

func (m *Model) loadHistory(conversation slack.Conversation) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		messages, hasMore, err := m.client.ConversationHistory(ctx, conversation.ID, 100)
		previews := m.loadKittyPreviews(ctx, messages)
		return historyLoadedMsg{conversation: conversation, messages: messages, previews: previews, hasMore: hasMore, err: err}
	}
}

func (m *Model) openDM(userID string) tview.Cmd {
	return func() tview.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conversation, err := m.client.OpenConversation(ctx, []string{userID})
		return dmOpenedMsg{conversation: conversation, err: err}
	}
}

func (m *Model) addDirectMessage(conversation slack.Conversation) {
	if m.directMessages == nil {
		m.directMessages = tree.NewNode("Direct messages").SetExpanded(true).SetSelectable(false)
	}
	if m.homeSidebarRoot == nil {
		m.homeSidebarRoot = m.sidebar.Root()
	}
	attached := false
	for _, node := range m.homeSidebarRoot.Children() {
		if node == m.directMessages {
			attached = true
			break
		}
	}
	if !attached {
		m.homeSidebarRoot.AddChild(m.directMessages)
	}
	for _, node := range m.directMessages.Children() {
		if id, ok := node.Reference().(string); ok && id == conversation.ID {
			return
		}
	}
	m.directMessages.AddChild(tree.NewNode("@ " + strings.TrimPrefix(m.conversationTitle(conversation), "@")).SetReference(conversation.ID))
	if m.dmSidebarRoot != nil {
		m.dmSidebarRoot.AddChild(tree.NewNode("@ " + strings.TrimPrefix(m.conversationTitle(conversation), "@")).SetReference(conversation.ID))
	}
}

func conversationActivity(conversation slack.Conversation) string {
	if conversation.LatestMessageTS != "" {
		return conversation.LatestMessageTS
	}
	if conversation.Updated > 0 {
		return fmt.Sprintf("%020d", conversation.Updated)
	}
	return fmt.Sprintf("%020d", conversation.Created*1000)
}

func (m *Model) isAppConversation(conversation slack.Conversation) bool {
	if !conversation.IsIM {
		return false
	}
	user, ok := m.users[conversation.User]
	return ok && (user.IsBot || user.ID == "USLACKBOT" || strings.EqualFold(user.Name, "slackbot"))
}

func (m *Model) conversationTitle(conversation slack.Conversation) string {
	name := conversation.Name
	if conversation.IsIM {
		if user, ok := m.users[conversation.User]; ok {
			name = displayName(user)
		}
	}
	if name == "" {
		name = conversation.ID
	}
	if conversation.IsIM || conversation.IsMPIM {
		return "@" + name
	}
	return "#" + name
}

func (m *Model) showConversationHeader(conversation slack.Conversation) {
	title := m.conversationTitle(conversation)
	details := strings.TrimSpace(m.renderSlackText(conversation.Topic.Value))
	if details == "" {
		details = strings.TrimSpace(m.renderSlackText(conversation.Purpose.Value))
	}
	if conversation.NumMembers > 0 && !conversation.IsIM {
		memberLabel := fmt.Sprintf("%d members", conversation.NumMembers)
		if details == "" {
			details = memberLabel
		} else {
			details += "  ·  " + memberLabel
		}
	}
	if details == "" {
		if conversation.IsIM {
			details = "Direct message"
		} else if conversation.IsMPIM {
			details = "Group direct message"
		} else if conversation.IsPrivate {
			details = "Private channel"
		} else {
			details = "Channel"
		}
	}
	m.header.SetText(title + "\n" + details)
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
