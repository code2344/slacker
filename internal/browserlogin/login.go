package browserlogin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
	"github.com/code2344/slacker/internal/slackdesktop"
	"github.com/golang/snappy"
	_ "modernc.org/sqlite"
)

type Session struct {
	TeamID string
	Token  string
	Cookie string
}

func Login(ctx context.Context) ([]Session, error) {
	if err := openDefaultBrowser("https://app.slack.com/client"); err != nil {
		return nil, fmt.Errorf("open the default browser: %w", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if result, err := discover(ctx); err == nil && len(result) > 0 {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("Slack sign-in was not detected in a supported browser: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func openDefaultBrowser(target string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{target}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		command, args = "xdg-open", []string{target}
	}
	return exec.Command(command, args...).Start()
}

func discover(ctx context.Context) ([]Session, error) {
	cookies, _ := kooky.ReadCookies(ctx, kooky.DomainHasSuffix("slack.com"), kooky.Name("d"))
	var cookieValues []string
	seenCookies := map[string]bool{}
	for _, candidate := range cookies {
		if candidate.Value != "" && !seenCookies[candidate.Value] {
			seenCookies[candidate.Value] = true
			cookieValues = append(cookieValues, candidate.Value)
		}
	}
	if len(cookieValues) == 0 {
		return nil, errors.New("Slack login cookie is not available yet")
	}
	tokens := map[string]string{}
	for _, dir := range chromiumLocalStorageDirs() {
		found, err := slackdesktop.TokensFromLevelDB(dir)
		if err != nil {
			continue
		}
		for teamID, token := range found {
			tokens[teamID] = token
		}
	}
	for _, dbPath := range firefoxLocalStorageDatabases() {
		found, err := firefoxTokens(dbPath)
		if err != nil {
			continue
		}
		for teamID, token := range found {
			tokens[teamID] = token
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("Slack browser token is not available yet")
	}
	result := make([]Session, 0, len(tokens)*len(cookieValues))
	for teamID, token := range tokens {
		for _, cookie := range cookieValues {
			result = append(result, Session{TeamID: teamID, Token: token, Cookie: cookie})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TeamID < result[j].TeamID })
	return result, nil
}

func firefoxLocalStorageDatabases() []string {
	home, _ := os.UserHomeDir()
	var profiles string
	switch runtime.GOOS {
	case "darwin":
		profiles = filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")
	case "windows":
		profiles = filepath.Join(os.Getenv("APPDATA"), "Mozilla", "Firefox", "Profiles")
	default:
		profiles = filepath.Join(home, ".mozilla", "firefox")
	}
	matches, _ := filepath.Glob(filepath.Join(profiles, "*", "storage", "default", "*slack.com*", "ls", "data.sqlite"))
	return matches
}

func firefoxTokens(dbPath string) (map[string]string, error) {
	source, err := os.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	tmp, err := os.CreateTemp("", "slacker-firefox-localstorage-*.sqlite")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.ReadFrom(source); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", tmpPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var compression int
	var value []byte
	if err := db.QueryRow(`SELECT compression_type, value FROM data WHERE key = 'localConfig_v2'`).Scan(&compression, &value); err != nil {
		return nil, err
	}
	if compression == 1 {
		value, err = snappy.Decode(nil, value)
		if err != nil {
			return nil, err
		}
	}
	var config struct {
		Teams map[string]struct {
			Token string `json:"token"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(value, &config); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for teamID, team := range config.Teams {
		if team.Token != "" {
			result[teamID] = team.Token
		}
	}
	if len(result) == 0 {
		return nil, errors.New("Firefox Slack local storage has no workspace token")
	}
	return result, nil
}

func chromiumLocalStorageDirs() []string {
	home, _ := os.UserHomeDir()
	local := os.Getenv("LOCALAPPDATA")
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		roots = []string{
			filepath.Join(base, "Google", "Chrome"), filepath.Join(base, "Arc", "User Data"),
			filepath.Join(base, "BraveSoftware", "Brave-Browser"), filepath.Join(base, "Microsoft Edge"),
			filepath.Join(base, "Chromium"), filepath.Join(base, "Vivaldi"),
		}
	case "windows":
		roots = []string{
			filepath.Join(local, "Google", "Chrome", "User Data"), filepath.Join(local, "Microsoft", "Edge", "User Data"),
			filepath.Join(local, "BraveSoftware", "Brave-Browser", "User Data"), filepath.Join(local, "Vivaldi", "User Data"),
		}
	default:
		base := filepath.Join(home, ".config")
		roots = []string{
			filepath.Join(base, "google-chrome"), filepath.Join(base, "chromium"),
			filepath.Join(base, "BraveSoftware", "Brave-Browser"), filepath.Join(base, "microsoft-edge"),
			filepath.Join(base, "vivaldi"),
		}
	}
	var result []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "*", "Local Storage", "leveldb"))
		result = append(result, matches...)
	}
	return result
}
