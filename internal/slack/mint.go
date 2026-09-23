package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

var apiTokenRE = regexp.MustCompile(`"api_token":"([^"]+)"`)

func MintToken(ctx context.Context, domain, cookie string) (string, error) {
	if domain == "" || cookie == "" {
		return "", fmt.Errorf("workspace domain and d cookie are required")
	}
	client := &http.Client{Transport: &BrowserTransport{}, Timeout: 20 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+".slack.com", nil)
	if err != nil {
		return "", err
	}
	req.AddCookie(&http.Cookie{Name: "d", Value: cookie})
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mint Slack token: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", err
	}
	match := apiTokenRE.FindSubmatch(body)
	if match == nil {
		return "", fmt.Errorf("mint Slack token: api_token was not present")
	}
	return string(match[1]), nil
}
