package slack

import (
	"net/http"
	"strings"
)

type BrowserTransport struct {
	Inner     http.RoundTripper
	UserAgent string
}

func (t *BrowserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	inner := t.Inner
	if inner == nil {
		inner = http.DefaultTransport
	}
	if req.URL == nil || !isSlackHost(req.URL.Hostname()) {
		return inner.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	headers := map[string]string{
		"User-Agent":      t.UserAgent,
		"Accept":          "*/*",
		"Accept-Language": "en-US,en;q=0.9",
		"Origin":          "https://app.slack.com",
		"Sec-Fetch-Site":  "same-site",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Dest":  "empty",
		"Cache-Control":   "no-cache",
		"Pragma":          "no-cache",
	}
	for key, value := range headers {
		if value != "" && clone.Header.Get(key) == "" {
			clone.Header.Set(key, value)
		}
	}
	return inner.RoundTrip(clone)
}

func isSlackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}
