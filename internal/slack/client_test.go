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
