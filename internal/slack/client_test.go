package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCallUsesBoundBrowserSessionWithoutMutatingParams(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth.test" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		gotForm = r.Form
		cookie, err := r.Cookie("d")
		if err != nil || cookie.Value != "xoxd-session" {
			t.Errorf("d cookie = %v, %v", cookie, err)
		}
		if got := r.Header.Get("Origin"); got != "" {
			t.Errorf("non-Slack test host received browser headers: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "team_id": "T1"})
	}))
	defer server.Close()

	params := url.Values{"hello": {"world"}}
	client, err := NewClient(
		Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"},
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	var result Auth
	if err := client.Call(context.Background(), "auth.test", params, &result); err != nil {
		t.Fatal(err)
	}
	if gotForm.Get("token") != "xoxc-token" || gotForm.Get("hello") != "world" {
		t.Fatalf("form = %#v", gotForm)
	}
	if params.Get("token") != "" {
		t.Fatal("Call mutated caller parameters")
	}
}

func TestCallReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	defer server.Close()
	client, err := NewClient(
		Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"},
		WithHTTPClient(server.Client()),
		WithRetries(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	err = client.Call(context.Background(), "auth.test", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Code != "invalid_auth" {
		t.Fatalf("error = %#v", err)
	}
}

func TestEmojiList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/emoji.list" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"ok":true,"emoji":{"party":"https://example.test/party.png","celebrate":"alias:party"}}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	emojis, err := client.EmojiList(context.Background())
	if err != nil || emojis["party"] == "" || emojis["celebrate"] != "alias:party" {
		t.Fatalf("EmojiList() = %#v, %v", emojis, err)
	}
}

func TestConversationHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/conversations.history" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("channel") != "C123" || r.Form.Get("limit") != "50" {
			t.Errorf("form = %#v", r.Form)
		}
		io.WriteString(w, `{"ok":true,"messages":[{"user":"U1","text":"hello","ts":"1710000000.000100","files":[{"name":"image.png","mimetype":"image/png"}],"reactions":[{"name":"wave","count":2}]}],"has_more":true}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	messages, hasMore, err := client.ConversationHistory(context.Background(), "C123", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(messages) != 1 || messages[0].Text != "hello" || len(messages[0].Files) != 1 || len(messages[0].Reactions) != 1 {
		t.Fatalf("messages = %#v, hasMore = %v", messages, hasMore)
	}
}

func TestConversationHistoryPageUsesAndReturnsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.Form.Get("cursor"); got != "older-page" {
			t.Errorf("cursor = %q", got)
		}
		io.WriteString(w, `{"ok":true,"messages":[{"text":"older","ts":"100.1"}],"has_more":true,"response_metadata":{"next_cursor":"oldest-page"}}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	messages, cursor, hasMore, err := client.ConversationHistoryPage(context.Background(), "C123", 100, "older-page")
	if err != nil || len(messages) != 1 || cursor != "oldest-page" || !hasMore {
		t.Fatalf("messages=%#v cursor=%q hasMore=%v err=%v", messages, cursor, hasMore, err)
	}
}

func TestMarkConversationFallsBackForIM(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/conversations.mark" {
			io.WriteString(w, `{"ok":false,"error":"unknown_method"}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("channel") != "D123" || r.Form.Get("ts") != "200.1" {
			t.Errorf("mark form = %#v", r.Form)
		}
		io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.MarkConversation(context.Background(), Conversation{ID: "D123", IsIM: true}, "200.1"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(paths, ",") != "/api/conversations.mark,/api/im.mark" {
		t.Fatalf("mark paths = %#v", paths)
	}
}

func TestPostMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat.postMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("channel") != "C123" || r.Form.Get("text") != "hello" || r.Form.Get("mrkdwn") != "true" || r.Form.Get("client_msg_id") == "" {
			t.Errorf("form = %#v", r.Form)
		}
		io.WriteString(w, `{"ok":true,"message":{"user":"U1","text":"hello","ts":"1710000000.1"}}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	message, err := client.PostMessage(context.Background(), "C123", "hello", "")
	if err != nil || message.Text != "hello" {
		t.Fatalf("message = %#v, err = %v", message, err)
	}
}

func TestConversationRepliesAndThreadReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/conversations.replies":
			if r.Form.Get("channel") != "C123" || r.Form.Get("ts") != "1710000000.1" {
				t.Errorf("replies form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true,"messages":[{"text":"root","ts":"1710000000.1"},{"text":"reply","ts":"1710000001.1","thread_ts":"1710000000.1"}]}`)
		case "/api/chat.postMessage":
			if r.Form.Get("thread_ts") != "1710000000.1" {
				t.Errorf("post form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true,"message":{"text":"new reply","ts":"1710000001.1","thread_ts":"1710000000.1"}}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	messages, _, err := client.ConversationReplies(context.Background(), "C123", "1710000000.1", 100)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %#v, err = %v", messages, err)
	}
	if _, err := client.PostMessage(context.Background(), "C123", "new reply", "1710000000.1"); err != nil {
		t.Fatal(err)
	}
}

func TestMessageMutations(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, r.URL.Path)
		if r.Form.Get("channel") != "C123" {
			t.Errorf("channel = %q", r.Form.Get("channel"))
		}
		switch r.URL.Path {
		case "/api/chat.update":
			if r.Form.Get("ts") != "1710000000.1" || r.Form.Get("text") != "edited" {
				t.Errorf("update form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true,"message":{"text":"edited"}}`)
		case "/api/chat.delete":
			if r.Form.Get("ts") != "1710000000.1" {
				t.Errorf("delete form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true}`)
		case "/api/reactions.add", "/api/reactions.remove":
			if r.Form.Get("timestamp") != "1710000000.1" || r.Form.Get("name") != "thumbsup" {
				t.Errorf("reaction form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.UpdateMessage(context.Background(), "C123", "1710000000.1", "edited"); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteMessage(context.Background(), "C123", "1710000000.1"); err != nil {
		t.Fatal(err)
	}
	if err := client.AddReaction(context.Background(), "C123", "1710000000.1", "thumbsup"); err != nil {
		t.Fatal(err)
	}
	if err := client.RemoveReaction(context.Background(), "C123", "1710000000.1", "thumbsup"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths, ","); got != "/api/chat.update,/api/chat.delete,/api/reactions.add,/api/reactions.remove" {
		t.Fatalf("paths = %q", got)
	}
}

func TestListChannelSections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users.channelSections.list" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"ok":true,"channel_sections":[{"channel_section_id":"S1","name":"Projects","channel_ids_page":{"channel_ids":["C2","C1"],"count":2}}]}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	sections, err := client.ListChannelSections(context.Background())
	if err != nil || len(sections) != 1 || len(sections[0].ChannelIDsPage.ChannelIDs) != 2 {
		t.Fatalf("sections = %#v, err = %v", sections, err)
	}
}

func TestOpenConversation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/conversations.open" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("users") != "U123" || r.Form.Get("return_im") != "true" {
			t.Errorf("form = %#v", r.Form)
		}
		io.WriteString(w, `{"ok":true,"channel":{"id":"D123","is_im":true,"user":"U123"}}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := client.OpenConversation(context.Background(), []string{"U123"})
	if err != nil || conversation.ID != "D123" || !conversation.IsIM || conversation.User != "U123" {
		t.Fatalf("conversation = %#v, err = %v", conversation, err)
	}
}

func TestOpenConversationRequiresUser(t *testing.T) {
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenConversation(context.Background(), nil); err == nil {
		t.Fatal("expected an error for an empty user list")
	}
}

func TestUserInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/users.info" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("user") != "U123" {
			t.Errorf("user = %q", r.Form.Get("user"))
		}
		io.WriteString(w, `{"ok":true,"user":{"id":"U123","name":"ruben","tz":"Australia/Hobart","tz_offset":39600,"profile":{"display_name":"Ruben","real_name":"Ruben Sutton","email":"ruben@example.com","phone":"123","start_date":"2026-03-02","fields":{"X1":{"value":"Hobart","alt":""}}}}}`)
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	user, err := client.UserInfo(context.Background(), "U123")
	if err != nil || user.ID != "U123" || user.Profile.DisplayName != "Ruben" || user.Profile.Email != "ruben@example.com" || user.Profile.Fields["X1"].Value != "Hobart" || !user.ProfileLoaded {
		t.Fatalf("user = %#v, err = %v", user, err)
	}
}

func TestProfileSupportingMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/team.profile.get":
			io.WriteString(w, `{"ok":true,"profile":{"fields":[{"id":"X1","ordering":2,"label":"Location","type":"text"}]}}`)
		case "/api/users.getPresence":
			if r.Form.Get("user") != "U123" {
				t.Errorf("presence user = %q", r.Form.Get("user"))
			}
			io.WriteString(w, `{"ok":true,"presence":"active","online":true}`)
		case "/api/dnd.info":
			if r.Form.Get("user") != "U123" {
				t.Errorf("dnd user = %q", r.Form.Get("user"))
			}
			io.WriteString(w, `{"ok":true,"dnd_enabled":true,"snooze_enabled":true,"snooze_endtime":1710000000}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	fields, err := client.TeamProfile(context.Background())
	if err != nil || len(fields) != 1 || fields[0].Label != "Location" {
		t.Fatalf("fields = %#v, err = %v", fields, err)
	}
	presence, err := client.UserPresence(context.Background(), "U123")
	if err != nil || presence.Presence != "active" || !presence.Online {
		t.Fatalf("presence = %#v, err = %v", presence, err)
	}
	dnd, err := client.DNDInfo(context.Background(), "U123")
	if err != nil || !dnd.SnoozeEnabled {
		t.Fatalf("dnd = %#v, err = %v", dnd, err)
	}
}

func TestSearchAndAuthenticatedDownload(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search.messages":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Form.Get("query") != "needle" {
				t.Errorf("message query = %q", r.Form.Get("query"))
			}
			io.WriteString(w, `{"ok":true,"messages":{"matches":[{"text":"needle","ts":"1710000000.1","channel":{"id":"C1","name":"general"}}],"total":1}}`)
		case "/api/search.files":
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			io.WriteString(w, `{"ok":true,"files":{"matches":[{"id":"F1","name":"needle.png","mimetype":"image/png","url_private_download":"`+server.URL+`/download"}],"total":1}}`)
		case "/download":
			if got := r.Header.Get("Authorization"); got != "Bearer xoxc-token" {
				t.Errorf("authorization = %q", got)
			}
			cookie, err := r.Cookie("d")
			if err != nil || cookie.Value != "xoxd-session" {
				t.Errorf("cookie = %#v, err = %v", cookie, err)
			}
			io.WriteString(w, "image bytes")
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	messages, total, err := client.SearchMessages(context.Background(), "needle")
	if err != nil || total != 1 || len(messages) != 1 || messages[0].Channel.Name != "general" {
		t.Fatalf("messages = %#v, total = %d, err = %v", messages, total, err)
	}
	files, total, err := client.SearchFiles(context.Background(), "needle")
	if err != nil || total != 1 || len(files) != 1 {
		t.Fatalf("files = %#v, total = %d, err = %v", files, total, err)
	}
	var downloaded strings.Builder
	if err := client.Download(context.Background(), files[0].URLPrivateDownload, &downloaded); err != nil || downloaded.String() != "image bytes" {
		t.Fatalf("download = %q, err = %v", downloaded.String(), err)
	}
}

func TestActivityFeedAndMessageHydration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch r.URL.Path {
		case "/api/activity.feed":
			if !strings.Contains(r.Form.Get("types"), "message_reaction") || r.Form.Get("mode") != "chrono_v1" {
				t.Errorf("activity form = %#v", r.Form)
			}
			io.WriteString(w, `{"ok":true,"items":[{"key":"mention-1","is_unread":true,"feed_ts":"1710000001.0","item":{"type":"at_user","message":{"channel":"C1","ts":"1710000000.1","author_user_id":"U2"}}},{"key":"reaction-1","item":{"type":"message_reaction","message":{"channel":"C1","ts":"1710000000.1"},"reaction":{"user":"U3","name":"heart"}}}],"response_metadata":{"next_cursor":"next"}}`)
		case "/api/messages.list":
			if !strings.Contains(r.Form.Get("message_ids"), `"channel":"C1"`) {
				t.Errorf("message ids = %q", r.Form.Get("message_ids"))
			}
			io.WriteString(w, `{"ok":true,"messages_data":{"C1":{"messages":[{"ts":"1710000000.1","user":"U2","text":"hello <@U1>"}]}}}`)
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewClient(Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: server.URL + "/api/"}, WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	feed, err := client.ActivityFeed(context.Background(), 50, "", false)
	if err != nil || len(feed.Items) != 2 || feed.Items[0].AuthorID != "U2" || feed.Items[1].Reaction != "heart" || feed.NextCursor != "next" {
		t.Fatalf("feed = %#v, err = %v", feed, err)
	}
	bodies, err := client.ActivityMessages(context.Background(), map[string][]string{"C1": {"1710000000.1"}})
	if err != nil || bodies[ActivityMessageKey("C1", "1710000000.1")].Text != "hello <@U1>" {
		t.Fatalf("bodies = %#v, err = %v", bodies, err)
	}
}

func TestConnectDoesNotAdoptEnterpriseNavigationURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/client.boot" {
			t.Errorf("path = %q", r.URL.Path)
		}
		io.WriteString(w, `{"ok":true,"self":{"id":"U1","name":"ruben","team_id":"T1"},"team":{"id":"T1","name":"Example","url":"https://example.slack.com/"}}`)
	}))
	defer server.Close()
	apiURL := server.URL + "/api/"
	client, err := NewClient(
		Session{Token: "xoxc-token", Cookie: "xoxd-session", APIURL: apiURL},
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if auth.TeamID != "T1" || auth.UserID != "U1" {
		t.Fatalf("auth = %#v", auth)
	}
	if client.APIURL() != apiURL {
		t.Fatalf("API URL changed to %q", client.APIURL())
	}
}

func TestBrowserTransportUsesFirefoxIdentityWithoutChromiumHints(t *testing.T) {
	const firefoxUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:145.0) Gecko/20100101 Firefox/145.0"
	inner := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("User-Agent"); got != firefoxUA {
			t.Errorf("User-Agent = %q", got)
		}
		if got := req.Header.Get("Sec-Ch-Ua-Platform"); got != "" {
			t.Errorf("Firefox request received Chromium client hint %q", got)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: req}, nil
	})
	transport := &BrowserTransport{Inner: inner, UserAgent: firefoxUA}
	req, _ := http.NewRequest(http.MethodPost, "https://slack.com/api/client.boot", nil)
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserTransportOnlyDecoratesSlackHosts(t *testing.T) {
	inner := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(`{"ok":true}`))
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: body, Request: req}, nil
	})
	transport := &BrowserTransport{Inner: inner}
	for _, test := range []struct {
		url      string
		decorate bool
	}{
		{"https://slack.com/api/auth.test", true},
		{"https://example.slack.com/api/auth.test", true},
		{"https://slack.com.example.test/api/auth.test", false},
	} {
		req, _ := http.NewRequest(http.MethodPost, test.url, nil)
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		got := resp.Request.Header.Get("Origin") != ""
		if got != test.decorate {
			t.Errorf("%s decorated = %v, want %v", test.url, got, test.decorate)
		}
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
