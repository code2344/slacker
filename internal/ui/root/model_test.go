package root

import (
	"strings"
	"testing"

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
	m := &Model{
		users: map[string]slack.User{
			"U123": {ID: "U123", Profile: struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
				Pronouns    string `json:"pronouns"`
				Title       string `json:"title"`
				StatusText  string `json:"status_text"`
				StatusEmoji string `json:"status_emoji"`
				Image48     string `json:"image_48"`
			}{DisplayName: "Ruben"}},
		},
		conversations: map[string]slack.Conversation{"C123": {ID: "C123", Name: "general"}},
	}
	got := m.renderSlackText("Hi <@U123> in <#C123> — *bold* &amp; <https://example.com|a link>")
	want := "Hi @Ruben in #general — bold & a link (https://example.com)"
	if got != want {
		t.Fatalf("renderSlackText() = %q, want %q", got, want)
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
