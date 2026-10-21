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
