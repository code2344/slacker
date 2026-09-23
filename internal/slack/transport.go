package slack

import (
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

type BrowserTransport struct{ Inner http.RoundTripper }

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
		"User-Agent":         userAgent(),
		"Accept":             "*/*",
		"Accept-Language":    "en-US,en;q=0.9",
		"Origin":             "https://app.slack.com",
		"Sec-Fetch-Site":     "same-site",
		"Sec-Fetch-Mode":     "cors",
		"Sec-Fetch-Dest":     "empty",
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": platformHint(),
		"Cache-Control":      "no-cache",
		"Pragma":             "no-cache",
	}
	for key, value := range headers {
		if clone.Header.Get(key) == "" {
			clone.Header.Set(key, value)
		}
	}
	return inner.RoundTrip(clone)
}

func isSlackHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "slack.com" || strings.HasSuffix(host, ".slack.com")
}

func userAgent() string {
	osPart := "X11; Linux x86_64"
	switch runtime.GOOS {
	case "darwin":
		osPart = "Macintosh; Intel Mac OS X 10_15_7"
	case "windows":
		osPart = "Windows NT 10.0; Win64; x64"
	}
	return fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36", osPart)
}

func platformHint() string {
	switch runtime.GOOS {
	case "darwin":
		return `"macOS"`
	case "windows":
		return `"Windows"`
	default:
		return `"Linux"`
	}
}
