package browserlogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/code2344/slacker/internal/consts"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

type Session struct {
	TeamID string
	Name   string
	Domain string
	Token  string
	Cookie string
}

type localConfig struct {
	Teams map[string]struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Domain string `json:"domain"`
		URL    string `json:"url"`
		Token  string `json:"token"`
	} `json:"teams"`
}

// Login opens an isolated, persistent Chromium profile for an interactive
// Slack sign-in and returns the web-client sessions created by Slack itself.
func Login(ctx context.Context) ([]Session, error) {
	if err := os.MkdirAll(consts.CacheDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create login browser profile: %w", err)
	}
	profile, err := os.MkdirTemp(consts.CacheDir(), "login-browser-")
	if err != nil {
		return nil, fmt.Errorf("create login browser profile: %w", err)
	}
	defer os.RemoveAll(profile)
	controlURL, err := launcher.New().Context(ctx).Headless(false).Leakless(false).
		UserDataDir(profile).Launch()
	if err != nil {
		return nil, fmt.Errorf("open login browser (Chrome or Chromium is required): %w", err)
	}
	browser := rod.New().Context(ctx).ControlURL(controlURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to login browser: %w", err)
	}
	defer browser.Close()
	page, err := browser.Page(proto.TargetCreateTarget{URL: "https://app.slack.com/client"})
	if err != nil {
		return nil, fmt.Errorf("open Slack sign-in: %w", err)
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		config, cookie, err := capture(browser, page)
		if err == nil {
			return sessions(config, cookie)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Slack browser sign-in did not finish: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func capture(browser *rod.Browser, page *rod.Page) (localConfig, string, error) {
	result, err := page.Eval(`() => localStorage.getItem("localConfig_v2") || ""`)
	if err != nil || result.Value.Str() == "" {
		return localConfig{}, "", errors.New("Slack session is not ready")
	}
	var config localConfig
	if err := json.Unmarshal([]byte(result.Value.Str()), &config); err != nil || len(config.Teams) == 0 {
		return localConfig{}, "", errors.New("Slack workspace data is not ready")
	}
	cookies, err := browser.GetCookies()
	if err != nil {
		return localConfig{}, "", err
	}
	for _, cookie := range cookies {
		if cookie.Name == "d" && strings.HasSuffix(cookie.Domain, "slack.com") && cookie.Value != "" {
			return config, cookie.Value, nil
		}
	}
	return localConfig{}, "", errors.New("Slack login cookie is not ready")
}

func sessions(config localConfig, cookie string) ([]Session, error) {
	var out []Session
	for key, team := range config.Teams {
		if team.Token == "" {
			continue
		}
		teamID := team.ID
		if teamID == "" {
			teamID = key
		}
		domain := team.Domain
		if domain == "" && team.URL != "" {
			if parsed, err := url.Parse(team.URL); err == nil {
				domain = strings.TrimSuffix(parsed.Hostname(), ".slack.com")
			}
		}
		if teamID != "" && domain != "" {
			out = append(out, Session{TeamID: teamID, Name: team.Name, Domain: domain, Token: team.Token, Cookie: cookie})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("Slack login completed without a usable workspace session")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
