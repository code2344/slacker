package cmd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/code2344/slacker/internal/consts"
	"github.com/code2344/slacker/internal/slack"
)

func TestStartupProgressShowsStage(t *testing.T) {
	var output bytes.Buffer
	progress := &startupProgress{w: &output, total: 4, enabled: true}
	progress.Step("Loading conversations")
	if got := output.String(); !strings.Contains(got, "1/4") || !strings.Contains(got, "Loading conversations") {
		t.Fatalf("progress output = %q", got)
	}
}

func TestDMActivityCacheFreshness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dm-activity.json")
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	writeDMActivityCache(path, dmActivityCache{UpdatedAt: now, Latest: map[string]string{"D1": "123.45", "D2": ""}})

	cache, fresh := readDMActivityCache(path, now.Add(time.Minute))
	if !fresh || cache.Latest["D1"] != "123.45" {
		t.Fatalf("fresh cache = %#v, fresh=%v", cache, fresh)
	}
	if _, fresh = readDMActivityCache(path, now.Add(3*time.Minute)); fresh {
		t.Fatal("expired DM activity cache reported fresh")
	}
}

func TestStaleDMActivityCacheDoesNotBlockStartup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		fmt.Fprint(w, `{"ok":true,"messages":[]}`)
	}))
	defer server.Close()
	client, err := slack.NewClient(slack.Session{Token: "xoxc-test", Cookie: "xoxd-test", APIURL: server.URL + "/api/"}, slack.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	teamID := "T-STALE-CACHE-TEST"
	cachePath := filepath.Join(consts.CacheDir(), "dm-activity-"+teamID+".json")
	t.Cleanup(func() { _ = os.Remove(cachePath) })
	writeDMActivityCache(cachePath, dmActivityCache{UpdatedAt: time.Now().Add(-time.Hour), Latest: map[string]string{"D1": "123.45"}})
	started := time.Now()
	conversations := loadRecentDMActivity(context.Background(), client, []slack.Conversation{{ID: "D1", IsIM: true}}, teamID, 0)
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("stale cache blocked startup for %v", elapsed)
	}
	if !conversations[0].ActivityChecked || conversations[0].LatestMessageTS != "123.45" {
		t.Fatalf("cached activity was not applied: %#v", conversations[0])
	}
}

func TestListAllConversationsFollowsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("cursor") == "next-page" {
			fmt.Fprint(w, `{"ok":true,"channels":[{"id":"D1","is_im":true,"user":"U1"}],"response_metadata":{"next_cursor":""}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"channels":[{"id":"C1","name":"general"}],"response_metadata":{"next_cursor":"next-page"}}`)
	}))
	defer server.Close()
	client, err := slack.NewClient(slack.Session{Token: "xoxc-test", Cookie: "xoxd-test", APIURL: server.URL + "/api/"}, slack.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	conversations, err := listAllConversations(context.Background(), client)
	if err != nil || len(conversations) != 2 || conversations[1].ID != "D1" {
		t.Fatalf("conversations = %#v, err = %v", conversations, err)
	}
}

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
