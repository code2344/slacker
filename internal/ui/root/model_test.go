package root

import (
	"strings"
	"testing"
	"time"

	"github.com/ayn2op/tview/tree"
	"github.com/code2344/slacker/internal/slack"
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

func TestConversationTitleUsesDMDisplayName(t *testing.T) {
	user := slack.User{ID: "U123", Name: "ruben"}
	user.Profile.DisplayName = "Ruben Sutton"
	m := &Model{users: map[string]slack.User{"U123": user}}
	if got := m.conversationTitle(slack.Conversation{ID: "D123", IsIM: true, User: "U123"}); got != "@Ruben Sutton" {
		t.Fatalf("DM title = %q", got)
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

func TestOwnReactionNamesAndRendering(t *testing.T) {
	reactions := []slack.Reaction{{Name: "wave", Count: 2, Users: []string{"U1", "U2"}}, {Name: "eyes", Count: 1, Users: []string{"U2"}}}
	if got := strings.Join(ownReactionNames(reactions, "U1"), ","); got != "wave" {
		t.Fatalf("own reactions = %q", got)
	}
	if got := renderReactions(reactions, "U1"); got != ":wave: 2*  :eyes: 1" {
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
	if _, ok := children[1].Reference().(messageProfileReference); !ok {
		t.Fatalf("first message reference = %T", children[1].Reference())
	}
	if _, ok := children[3].Reference().(messageReference); !ok {
		t.Fatalf("compact message reference = %T", children[3].Reference())
	}
	if len(children[1].Children()) != 0 {
		t.Fatalf("first message has %d nested rows", len(children[1].Children()))
	}
	if _, ok := children[2].Reference().(threadReference); !ok {
		t.Fatalf("reply row reference = %T", children[2].Reference())
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
