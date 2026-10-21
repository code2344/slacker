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
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all"
	"github.com/code2344/slacker/internal/slackdesktop"
	"github.com/golang/snappy"
	_ "modernc.org/sqlite"
)

type Session struct {
	TeamID    string
	Token     string
	Cookie    string
	UserAgent string
}

type browserCookie struct {
	value, browser, profile string
}

type browserToken struct {
	teamID, token, browser, profile, userAgent string
}

func Login(ctx context.Context) ([]Session, error) {
	if err := checkFirefoxProfileAccess(); err != nil {
		return nil, err
	}
	if err := openDefaultBrowser("https://app.slack.com/client"); err != nil {
		return nil, fmt.Errorf("open the default browser: %w", err)
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastError := ""
	for {
		if result, err := discover(ctx); err == nil && len(result) > 0 {
			return result, nil
		} else if err != nil {
			lastError = err.Error()
		}
		select {
		case <-ctx.Done():
			if lastError != "" {
				return nil, fmt.Errorf("Slack sign-in was not detected: %s: %w", lastError, ctx.Err())
			}
			return nil, fmt.Errorf("Slack sign-in was not detected in a supported browser: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func checkFirefoxProfileAccess() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, "Library", "Application Support", "Firefox")
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.ReadDir(root); err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("macOS blocked access to the Firefox profile at %s; add your terminal application to System Settings > Privacy & Security > Full Disk Access, restart the terminal, and try again", root)
		}
		return fmt.Errorf("read Firefox profile directory: %w", err)
	}
	return nil
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
	cookies, err := kooky.ReadCookies(ctx, kooky.DomainHasSuffix("slack.com"), kooky.Name("d"))
	if err != nil {
		return nil, fmt.Errorf("read browser cookies: %w", err)
	}
	var cookieValues []browserCookie
	seenCookies := map[string]bool{}
	for _, candidate := range cookies {
		if candidate.Value != "" && !seenCookies[candidate.Value] {
			seenCookies[candidate.Value] = true
			current := browserCookie{value: candidate.Value}
			if candidate.Browser != nil {
				current.browser = strings.ToLower(candidate.Browser.Browser())
				current.profile = profileFromPath(candidate.Browser.FilePath())
			}
			cookieValues = append(cookieValues, current)
		}
	}
	if len(cookieValues) == 0 {
		return nil, errors.New("Slack login cookie is not available yet")
	}
	var tokens []browserToken
	for _, dir := range chromiumLocalStorageDirs() {
		found, err := slackdesktop.TokensFromLevelDB(dir)
		if err != nil {
			continue
		}
		for teamID, token := range found {
			tokens = append(tokens, browserToken{teamID: teamID, token: token, browser: "chromium", profile: profileFromPath(dir)})
		}
	}
	for _, dbPath := range firefoxLocalStorageDatabases() {
		found, err := firefoxTokens(dbPath)
		if err != nil {
			continue
		}
		for teamID, token := range found {
			tokens = append(tokens, browserToken{teamID: teamID, token: token, browser: "firefox", profile: profileFromPath(dbPath), userAgent: firefoxUserAgent()})
		}
	}
	if len(tokens) == 0 {
		return nil, errors.New("Slack browser token is not available yet")
	}
	result := make([]Session, 0, len(tokens))
	for _, token := range tokens {
		for _, cookie := range cookieValues {
			if token.browser == "firefox" && cookie.browser != "" && cookie.browser != "firefox" {
				continue
			}
			if token.profile != "" && cookie.profile != "" && token.profile != cookie.profile {
				continue
			}
			result = append(result, Session{TeamID: token.teamID, Token: token.token, Cookie: cookie.value, UserAgent: token.userAgent})
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
	defer os.Remove(tmpPath + "-wal")
	defer os.Remove(tmpPath + "-shm")
	if _, err := tmp.ReadFrom(source); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyFile(dbPath+suffix, tmpPath+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
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

func copyFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := destination.ReadFrom(source); err != nil {
		destination.Close()
		return err
	}
	return destination.Close()
}

func profileFromPath(value string) string {
	value = filepath.ToSlash(value)
	for _, marker := range []string{"/Profiles/", "/profiles/"} {
		if _, rest, ok := strings.Cut(value, marker); ok {
			return strings.Split(rest, "/")[0]
		}
	}
	return ""
}

var firefoxVersionRE = regexp.MustCompile(`(?m)^Version=([0-9]+(?:\.[0-9]+)?)`)

func firefoxUserAgent() string {
	if runtime.GOOS != "darwin" {
		return ""
	}
	home, _ := os.UserHomeDir()
	paths := []string{
		"/Applications/Firefox.app/Contents/Resources/application.ini",
		filepath.Join(home, "Applications", "Firefox.app", "Contents", "Resources", "application.ini"),
		filepath.Join(home, "Documents", "Apps", "Firefox.app", "Contents", "Resources", "application.ini"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		match := firefoxVersionRE.FindSubmatch(data)
		if len(match) == 2 {
			return fmt.Sprintf("Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:%s) Gecko/20100101 Firefox/%s", match[1], match[1])
		}
	}
	return ""
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
