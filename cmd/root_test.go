package cmd

import (
	"testing"

	"github.com/code2344/slacker/internal/slack"
)

func TestMergeConversationsAddsBootDMsAndReplacesDuplicates(t *testing.T) {
	listed := []slack.Conversation{{ID: "C1", Name: "old"}}
	bootChannels := []slack.Conversation{{ID: "C1", Name: "general"}}
	bootDMs := []slack.Conversation{{ID: "D1", IsIM: true, User: "U1"}}
	merged := mergeConversations(listed, bootChannels, bootDMs)
	if len(merged) != 2 || merged[0].Name != "general" || merged[1].ID != "D1" {
		t.Fatalf("merged = %#v", merged)
	}
}

func TestParseSlackCurl(t *testing.T) {
	input := `curl 'https://slack.com/api/client.boot' -H 'User-Agent: Mozilla/5.0 Firefox/158.0' -H 'Cookie: b=one; d=xoxd-abc%2Fdef%3D%3D; x=two' --data-raw 'token=xoxc-123-456&flannel=4'`
	token, cookie, userAgent, _, err := parseSlackCurl(input)
	if err != nil {
		t.Fatal(err)
	}
	if token != "xoxc-123-456" {
		t.Fatalf("token = %q", token)
	}
	if cookie != "xoxd-abc%2Fdef%3D%3D" {
		t.Fatalf("cookie = %q", cookie)
	}
	if userAgent != "Mozilla/5.0 Firefox/158.0" {
		t.Fatalf("user agent = %q", userAgent)
	}
}
