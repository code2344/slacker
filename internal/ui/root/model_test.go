package root

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayn2op/tview"
	"github.com/ayn2op/tview/tree"
	"github.com/code2344/slacker/internal/config"
	"github.com/code2344/slacker/internal/slack"
	"github.com/gdamore/tcell/v3"
)

func TestRenderMessagesIncludesFilesReactionsAndThreads(t *testing.T) {
	messages := []slack.Message{{
		User: "U123", Text: "hello", Timestamp: "1710000000.000100", ReplyCount: 2,
		Files:     []slack.File{{Name: "photo.png", Mimetype: "image/png"}},
		Reactions: []slack.Reaction{{Name: "wave", Count: 3}},
	}}
	got := renderMessages(messages, true)
	for _, want := range []string{"latest messages", "U123", "hello", "photo.png", "image/png", ":wave: 3", "2 replies"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered messages missing %q:\n%s", want, got)
		}
	}
}

func TestOrderSectionsFollowsSlackLinkedOrder(t *testing.T) {
	sections := []slack.ChannelSection{{ID: "S2", Name: "Second"}, {ID: "S1", Name: "First", NextChannelSectionID: "S2"}}
	got := orderSections(sections)
	if len(got) != 2 || got[0].ID != "S1" || got[1].ID != "S2" {
		t.Fatalf("order = %#v", got)
	}
}

func TestRenderSlackTextResolvesEntitiesAndFormatting(t *testing.T) {
	user := slack.User{ID: "U123"}
	user.Profile.DisplayName = "Ruben"
	m := &Model{
		users:         map[string]slack.User{"U123": user},
		conversations: map[string]slack.Conversation{"C123": {ID: "C123", Name: "general"}},
	}
	got := m.renderSlackText("Hi <@U123> in <#C123> — *bold* &amp; <https://example.com|a link>")
	want := "Hi @Ruben in #general — bold & a link (https://example.com)"
	if got != want {
		t.Fatalf("renderSlackText() = %q, want %q", got, want)
	}
}

func TestRenderSlackTextConvertsKnownEmojiAndPreservesCustomEmoji(t *testing.T) {
	m := &Model{users: map[string]slack.User{}, conversations: map[string]slack.Conversation{}}
	got := m.renderSlackText(":wave: :workspace_custom_thing:")
	if got != "👋 :workspace_custom_thing:" {
		t.Fatalf("rendered emoji = %q", got)
	}
}

func TestRenderMrkdwnLineAppliesInlineStyles(t *testing.T) {
	line := renderMrkdwnLine("  ", "*bold* _italic_ ~gone~ `code`", true)
	if len(line) != 9 {
		t.Fatalf("segments = %#v", line)
	}
	if !line[1].Style.HasBold() || !line[3].Style.HasItalic() || !line[5].Style.HasStrikeThrough() {
		t.Fatalf("mrkdwn styles were not applied: %#v", line)
	}
	if !line[7].Style.HasReverse() {
		t.Fatalf("code style = %#v", line[7].Style)
	}
	if !line[8].Style.HasItalic() || line[8].Text != "  (edited)" {
		t.Fatalf("edited segment = %#v", line[8])
	}
}

func TestRenderMrkdwnBlocksAndQuotes(t *testing.T) {
	value := prepareMrkdwn("> quoted\nnormal\n>>> long\ncontinued")
	if value != "│ quoted ↵ normal ↵ │ long ↵ │ continued" {
		t.Fatalf("prepared block text = %q", value)
	}
	line := renderMrkdwnLine("", "```code```", false)
	if len(line) != 2 || line[1].Text != "code" || !line[1].Style.HasReverse() {
		t.Fatalf("code block = %#v", line)
	}
}

func TestConversationTitleUsesDMDisplayName(t *testing.T) {
	user := slack.User{ID: "U123", Name: "ruben"}
	user.Profile.DisplayName = "Ruben Sutton"
	m := &Model{users: map[string]slack.User{"U123": user}}
	if got := m.conversationTitle(slack.Conversation{ID: "D123", IsIM: true, User: "U123"}); got != "@Ruben Sutton" {
		t.Fatalf("DM title = %q", got)
	}
}

func TestConversationHeaderIncludesTopicAndMembers(t *testing.T) {
	m := &Model{header: tview.NewTextView(), users: map[string]slack.User{}, conversations: map[string]slack.Conversation{}}
	conversation := slack.Conversation{ID: "C1", Name: "lounge", NumMembers: 42}
	conversation.Topic.Value = "Hang out and chat"
	m.showConversationHeader(conversation)
	if got := m.header.Text(); got != "#lounge\nHang out and chat  ·  42 members" {
		t.Fatalf("header = %q", got)
	}
}

func TestKittyImagePayloadIsChunkedAndSized(t *testing.T) {
	payload := kittyImagePayload(make([]byte, 5000), 42, 40, 12)
	encoded := payload
	if !strings.Contains(encoded, "a=T,f=100,q=2,U=1,C=1,i=42,c=40,r=12,m=1;") {
		t.Fatalf("first Kitty chunk missing sizing: %q", encoded[:min(120, len(encoded))])
	}
	if !strings.Contains(encoded, "q=2,m=0;") {
		t.Fatal("final Kitty chunk is missing")
	}
}

func TestKittyPlaceholderEncodesImageRowsAndColumns(t *testing.T) {
	lines := kittyPlaceholderLines(kittyPreview{imageID: 42, columns: 2, rows: 2})
	if len(lines) != 2 || len(lines[0]) != 3 {
		t.Fatalf("placeholder dimensions = %d rows, %d segments", len(lines), len(lines[0]))
	}
	if lines[0][1].Text != string([]rune{0x10EEEE, kittyDiacritics[0], kittyDiacritics[0]}) || lines[1][2].Text != string([]rune{0x10EEEE, kittyDiacritics[1], kittyDiacritics[1]}) {
		t.Fatal("placeholder row/column diacritics are incorrect")
	}
}

func TestAddDirectMessageAttachesAndDeduplicatesSidebarGroup(t *testing.T) {
	user := slack.User{ID: "U123", Name: "ruben"}
	user.Profile.DisplayName = "Ruben"
	m := &Model{
		sidebar:       tree.NewModel().SetRoot(tree.NewNode("")),
		users:         map[string]slack.User{"U123": user},
		conversations: map[string]slack.Conversation{},
	}
	conversation := slack.Conversation{ID: "D123", IsIM: true, User: "U123"}
	m.addDirectMessage(conversation)
	m.addDirectMessage(conversation)
	if len(m.sidebar.Root().Children()) != 1 || len(m.directMessages.Children()) != 1 {
		t.Fatalf("sidebar groups = %d, DM rows = %d", len(m.sidebar.Root().Children()), len(m.directMessages.Children()))
	}
	if got := m.directMessages.Children()[0].Line()[0].Text; got != "@ Ruben" {
		t.Fatalf("DM row = %q", got)
	}
}

func TestDirectMessagesStayInDedicatedSidebarGroup(t *testing.T) {
	section := slack.ChannelSection{ID: "S1", Name: "Channels"}
	section.ChannelIDsPage.ChannelIDs = []string{"C1", "D1"}
	m := NewModel(&config.Config{}, "Team", "U0", []slack.Conversation{
		{ID: "C1", Name: "general", IsChannel: true},
		{ID: "D1", IsIM: true, User: "U1"},
	}, []slack.ChannelSection{section}, map[string]slack.User{"U1": {ID: "U1", Name: "ruben"}}, nil, nil, nil)
	if len(m.directMessages.Children()) != 1 || m.directMessages.Children()[0].Reference() != "D1" {
		t.Fatalf("DM rows = %#v", m.directMessages.Children())
	}
	if got := m.sidebar.Root().Children()[1]; got != m.directMessages {
		t.Fatal("direct messages group is not attached to the sidebar")
	}
}

func TestSidebarShowsOnlyRecentDMsAndSeparatesApps(t *testing.T) {
	users := map[string]slack.User{}
	conversations := make([]slack.Conversation, 0, 20)
	for i := 0; i < 18; i++ {
		id := fmt.Sprintf("U%d", i)
		users[id] = slack.User{ID: id, Name: fmt.Sprintf("user-%02d", i)}
		conversations = append(conversations, slack.Conversation{ID: fmt.Sprintf("D%d", i), IsIM: true, User: id, Updated: int64(i + 1)})
	}
	bot := slack.User{ID: "UBOT", Name: "helper", IsBot: true}
	users[bot.ID] = bot
	conversations = append(conversations, slack.Conversation{ID: "DBOT", IsIM: true, User: bot.ID, Updated: 100})

	m := NewModel(&config.Config{}, "Team", "U0", conversations, nil, users, nil, nil, nil)
	if got := len(m.directMessages.Children()); got != 16 {
		t.Fatalf("recent DM rows = %d, want 16", got)
	}
	if got := len(m.apps.Children()); got != 1 {
		t.Fatalf("app rows = %d, want 1", got)
	}
	if got := m.directMessages.Children()[0].Line()[0].Text; got != "@ user-17" {
		t.Fatalf("first recent DM = %q", got)
	}
	if got := len(m.dmSidebarRoot.Children()); got != 20 {
		t.Fatalf("dedicated DM view rows = %d, want 20", got)
	}
}

func TestSidebarUsesLatestMessageAndExcludesEmptyDMs(t *testing.T) {
	users := map[string]slack.User{
		"U1": {ID: "U1", Name: "recent"},
		"U2": {ID: "U2", Name: "opened-only"},
		"U3": {ID: "U3", Name: "older"},
	}
	m := NewModel(&config.Config{}, "Team", "U0", []slack.Conversation{
		{ID: "D1", IsIM: true, User: "U1", Updated: 1, ActivityChecked: true, LatestMessageTS: "200.000001"},
		{ID: "D2", IsIM: true, User: "U2", Updated: 999, ActivityChecked: true},
		{ID: "D3", IsIM: true, User: "U3", Updated: 500, ActivityChecked: true, LatestMessageTS: "100.000001"},
	}, nil, users, nil, nil, nil)
	if got := len(m.directMessages.Children()); got != 2 {
		t.Fatalf("recent DM rows = %d, want 2", got)
	}
	if got := m.directMessages.Children()[0].Line()[0].Text; got != "@ recent" {
		t.Fatalf("first DM = %q", got)
	}
	if got := m.directMessages.Children()[1].Line()[0].Text; got != "@ older" {
		t.Fatalf("second DM = %q", got)
	}
}

func TestSentMessageIsRenderedWithoutWaitingForHistory(t *testing.T) {
	conversation := slack.Conversation{ID: "D1", IsIM: true, User: "U2"}
	m := NewModel(&config.Config{}, "Team", "U1", []slack.Conversation{conversation}, nil, map[string]slack.User{
		"U1": {ID: "U1", Name: "me"},
		"U2": {ID: "U2", Name: "friend"},
	}, nil, nil, nil)
	m.activeConversation = conversation.ID
	m.currentMessages = []slack.Message{{User: "U2", Text: "before", Timestamp: "100.000001"}}
	cmd := m.Update(messageSentMsg{
		conversation: conversation,
		message:      slack.Message{Text: "just sent", Timestamp: "200.000001"},
	})
	if cmd != nil {
		t.Fatal("successful non-thread send unexpectedly scheduled a history reload")
	}
	if len(m.currentMessages) != 2 || m.currentMessages[0].Text != "just sent" || m.currentMessages[0].User != "U1" {
		t.Fatalf("current messages = %#v", m.currentMessages)
	}
}

func TestEnterInComposerDispatchesMessageSend(t *testing.T) {
	conversation := slack.Conversation{ID: "C1", Name: "general", IsChannel: true}
	m := NewModel(&config.Config{}, "Team", "U1", []slack.Conversation{conversation}, nil, nil, nil, nil, nil)
	m.activeConversation = conversation.ID
	m.composer.SetDisabled(false)
	m.composer.SetText("hello", true)
	m.main.SetFocus(mainMessagesIndex)
	m.right.SetFocus(rightComposerIndex)

	cmd := m.Update(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if cmd == nil {
		t.Fatal("Enter in the focused composer did not dispatch a send")
	}
	if got := m.composer.Title(); got != "Sending…" {
		t.Fatalf("composer title = %q", got)
	}
}

func TestMainComposerNeverSendsIntoOpenThread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("thread_ts"); got != "" {
			t.Errorf("main composer sent thread_ts %q", got)
		}
		io.WriteString(w, `{"ok":true,"channel":"C1","ts":"200.000001","message":{"text":"channel message","ts":"200.000001"}}`)
	}))
	defer server.Close()
	client, err := slack.NewClient(slack.Session{Token: "xoxc-test", Cookie: "xoxd-test", APIURL: server.URL + "/api/"}, slack.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	conversation := slack.Conversation{ID: "C1", Name: "general", IsChannel: true}
	m := NewModel(&config.Config{}, "Team", "U1", []slack.Conversation{conversation}, nil, nil, nil, nil, client)
	m.activeConversation = conversation.ID
	m.activeThread = "100.000001"
	m.composer.SetDisabled(false)
	m.composer.SetText("channel message", true)
	m.main.SetFocus(mainMessagesIndex)
	m.right.SetFocus(rightComposerIndex)

	cmd := m.Update(tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone))
	if cmd == nil {
		t.Fatal("Enter in the main composer did not dispatch a send")
	}
	result, ok := cmd().(messageSentMsg)
	if !ok || result.err != nil || result.thread != "" {
		t.Fatalf("send result = %#v", result)
	}
}

func TestOwnReactionNamesAndRendering(t *testing.T) {
	reactions := []slack.Reaction{{Name: "wave", Count: 2, Users: []string{"U1", "U2"}}, {Name: "eyes", Count: 1, Users: []string{"U2"}}}
	if got := strings.Join(ownReactionNames(reactions, "U1"), ","); got != "wave" {
		t.Fatalf("own reactions = %q", got)
	}
	if got := renderReactions(reactions, "U1"); got != "👋 2*  👀 1" {
		t.Fatalf("rendered reactions = %q", got)
	}
}

func TestMessagesUseProfileRowsAndExplicitThreadRows(t *testing.T) {
	user := slack.User{ID: "U1", Name: "ruben"}
	user.Profile.DisplayName = "Ruben"
	m := &Model{
		messages:      tree.NewModel(),
		users:         map[string]slack.User{"U1": user},
		conversations: map[string]slack.Conversation{"C1": {ID: "C1", Name: "lounge"}},
	}
	conversation := m.conversations["C1"]
	m.showMessages(conversation, []slack.Message{
		{User: "U1", Text: "second", Timestamp: "1710000120.0"},
		{User: "U1", Text: "first", Timestamp: "1710000000.0", ReplyCount: 2},
	}, false, false)
	children := m.messages.Root().Children()
	if len(children) != 4 {
		t.Fatalf("top-level rows = %d, want 4", len(children))
	}
	if _, ok := children[0].Reference().(messageProfileReference); !ok {
		t.Fatalf("author reference = %T", children[0].Reference())
	}
	if _, ok := children[1].Reference().(messageReference); !ok {
		t.Fatalf("message body reference = %T", children[1].Reference())
	}
	if _, ok := children[3].Reference().(messageReference); !ok {
		t.Fatalf("compact message reference = %T", children[3].Reference())
	}
	if _, ok := children[2].Reference().(threadReference); !ok {
		t.Fatalf("reply row reference = %T", children[2].Reference())
	}
	if m.messages.CurrentNode() != children[3] {
		t.Fatal("message list did not select the newest message")
	}
	for _, child := range children {
		for _, segment := range child.Line() {
			if strings.Contains(segment.Text, "Profile:") {
				t.Fatalf("found obsolete profile child %q", segment.Text)
			}
		}
	}
}

func TestFormatRelativeTimestamp(t *testing.T) {
	now := time.Unix(1710000180, 0).Local()
	if got := formatRelativeTimestamp("1710000000.0", now); got != "3 minutes ago" {
		t.Fatalf("relative timestamp = %q", got)
	}
}

func TestUnresolvedUserIDsIncludesPartialUsersOnce(t *testing.T) {
	m := &Model{
		users:       map[string]slack.User{"U1": {ID: "U1", Name: "partial"}},
		userLookups: map[string]bool{},
	}
	messages := []slack.Message{{User: "U1"}, {User: "U2"}, {User: "U2"}}
	if got := strings.Join(m.unresolvedUserIDs(messages), ","); got != "U1,U2" {
		t.Fatalf("unresolved IDs = %q", got)
	}
	if got := m.unresolvedUserIDs(messages); len(got) != 0 {
		t.Fatalf("second lookup returned %#v", got)
	}
}

func TestProfileRoles(t *testing.T) {
	user := slack.User{IsAdmin: true, IsRestricted: true, IsBot: true}
	if got := strings.Join(profileRoles(user), ","); got != "Admin,Guest,Bot" {
		t.Fatalf("roles = %q", got)
	}
}
