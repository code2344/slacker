package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPIURL = "https://slack.com/api/"

type Session struct {
	Token   string
	Cookie  string
	CookieS string
	APIURL  string
}

type Client struct {
	session Session
	http    *http.Client
	retries int
}

type APIError struct {
	Method string
	Code   string
}

func (e *APIError) Error() string { return fmt.Sprintf("Slack %s failed: %s", e.Method, e.Code) }

type Option func(*Client)

func WithHTTPClient(client *http.Client) Option { return func(c *Client) { c.http = client } }
func WithRetries(retries int) Option            { return func(c *Client) { c.retries = max(0, retries) } }

func NewClient(session Session, opts ...Option) (*Client, error) {
	if session.Token == "" || session.Cookie == "" {
		return nil, errors.New("Slack user token and d cookie are required")
	}
	if session.APIURL == "" {
		session.APIURL = defaultAPIURL
	}
	if _, err := url.ParseRequestURI(session.APIURL); err != nil {
		return nil, fmt.Errorf("invalid Slack API URL: %w", err)
	}
	c := &Client{session: session, http: &http.Client{Transport: &BrowserTransport{}, Timeout: 30 * time.Second}, retries: 2}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func (c *Client) APIURL() string { return c.session.APIURL }

// Connect validates the browser session and adopts the workspace-specific API
// host returned by Slack. Enterprise Grid requests rely on this routing.
func (c *Client) Connect(ctx context.Context) (Auth, error) {
	auth, err := c.AuthTest(ctx)
	if err != nil {
		return Auth{}, err
	}
	if auth.URL != "" {
		workspaceURL, err := url.Parse(auth.URL)
		if err != nil {
			return Auth{}, fmt.Errorf("parse workspace URL: %w", err)
		}
		workspaceURL.Path = "/api/"
		workspaceURL.RawQuery = ""
		workspaceURL.Fragment = ""
		c.session.APIURL = workspaceURL.String()
	}
	return auth, nil
}

func (c *Client) Call(ctx context.Context, method string, params url.Values, out any) error {
	if method == "" || strings.ContainsAny(method, "/?#") {
		return fmt.Errorf("invalid Slack method %q", method)
	}
	if params == nil {
		params = url.Values{}
	} else {
		params = cloneValues(params)
	}
	params.Set("token", c.session.Token)

	for attempt := 0; ; attempt++ {
		err, retryAfter := c.callOnce(ctx, method, params, out)
		if err == nil {
			return nil
		}
		var apiErr *APIError
		retryable := errors.As(err, &apiErr) && (apiErr.Code == "ratelimited" || apiErr.Code == "rate_limited")
		if attempt >= c.retries || (!retryable && retryAfter == 0) {
			return err
		}
		if retryAfter <= 0 {
			retryAfter = 2 * time.Second
		}
		timer := time.NewTimer(min(retryAfter, 30*time.Second))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

func (c *Client) callOnce(ctx context.Context, method string, params url.Values, out any) (error, time.Duration) {
	base, err := url.Parse(c.session.APIURL)
	if err != nil {
		return err, 0
	}
	endpoint := base.ResolveReference(&url.URL{Path: method})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(params.Encode()))
	if err != nil {
		return err, 0
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	req.AddCookie(&http.Cookie{Name: "d", Value: c.session.Cookie})
	if c.session.CookieS != "" {
		req.AddCookie(&http.Cookie{Name: "d-s", Value: c.session.CookieS})
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call Slack %s: %w", method, err), 0
	}
	defer resp.Body.Close()
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	if resp.StatusCode == http.StatusTooManyRequests {
		return &APIError{Method: method, Code: "ratelimited"}, retryAfter
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Slack %s returned HTTP %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body))), 0
	}
	var envelope struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err, 0
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("decode Slack %s response: %w", method, err), 0
	}
	if !envelope.OK {
		return &APIError{Method: method, Code: envelope.Error}, retryAfter
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decode Slack %s result: %w", method, err), 0
		}
	}
	return nil, 0
}

func cloneValues(values url.Values) url.Values {
	clone := make(url.Values, len(values)+1)
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

func parseRetryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
