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

func TestPostMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat.postMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("channel") != "C123" || r.Form.Get("text") != "hello" {
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
			io.WriteString(w, `{"ok":true,"message":{"text":"new reply","thread_ts":"1710000000.1"}}`)
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
