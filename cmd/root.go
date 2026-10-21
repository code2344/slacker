package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ayn2op/tview"
	"github.com/code2344/slacker/internal/browserlogin"
	"github.com/code2344/slacker/internal/config"
	"github.com/code2344/slacker/internal/logger"
	"github.com/code2344/slacker/internal/slack"
	"github.com/code2344/slacker/internal/slackdesktop"
	uiroot "github.com/code2344/slacker/internal/ui/root"
	"github.com/code2344/slacker/internal/workspace"
	"github.com/gdamore/tcell/v3"
	"github.com/mattn/go-shellwords"
	"golang.org/x/term"
)

func Run() error { return run(os.Args[1:]) }

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "workspace":
			return runWorkspace(args[1:])
		case "doctor":
			return runDoctor()
		case "api":
			return runAPI(args[1:])
		case "version", "--version", "-v":
			fmt.Println(buildVersion())
			return nil
		case "help", "--help", "-h":
			printUsage()
			return nil
		}
	}

	flags := flag.NewFlagSet("slacker", flag.ContinueOnError)
	configPath := flags.String("config-path", config.DefaultPath(), "path to config.toml")
	logPath := flags.String("log-path", logger.DefaultPath(), "path to the log file")
	debug := flags.Bool("debug", false, "enable debug logging")
	send := flags.Bool("send", false, "send a message and exit")
	flags.BoolVar(send, "s", false, "send a message and exit")
	channel := flags.String("channel", "", "channel name or ID")
	flags.StringVar(channel, "c", "", "channel name or ID")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *send {
		return runSend(*channel, strings.Join(flags.Args(), " "))
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logFile, err := logger.Load(*logPath, level)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	return runTUI(cfg)
}

func runWorkspace(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: slacker workspace <add|import-curl|link|diagnose|list|current|use|remove|refresh>")
	}
	store := workspace.New(workspace.DefaultPath())
	switch args[0] {
	case "list":
		items, active, err := store.List()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Println("No workspaces configured. Run `slacker workspace add`.")
			return nil
		}
		for _, item := range items {
			marker := " "
			if item.TeamID == active {
				marker = "*"
			}
			fmt.Printf("%s %-24s %-18s %s\n", marker, item.Name, item.Domain, item.TeamID)
		}
		return nil
	case "current":
		meta, _, err := store.Active()
		if err != nil {
			return err
		}
		fmt.Printf("%s (%s, %s)\n", meta.Name, meta.Domain, meta.TeamID)
		return nil
	case "use":
		if len(args) != 2 {
			return errors.New("usage: slacker workspace use <name|domain|team-id>")
		}
		return store.Use(args[1])
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: slacker workspace remove <name|domain|team-id>")
		}
		return store.Remove(args[1])
	case "refresh":
		return refreshWorkspace(store)
	case "add":
		return addWorkspace(store, args[1:])
	case "link":
		return linkWorkspace(store, args[1:])
	case "diagnose":
		return diagnoseWorkspace(args[1:])
	case "import-curl":
		return importCurlWorkspace(store, args[1:])
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
}

func importCurlWorkspace(store *workspace.Store, args []string) error {
	flags := flag.NewFlagSet("workspace import-curl", flag.ContinueOnError)
	domain := flags.String("domain", "", "workspace URL or subdomain")
	if err := flags.Parse(args); err != nil {
		return err
	}
	input, err := curlInput()
	if err != nil {
		return err
	}
	token, cookie, userAgent, detectedDomain, err := parseSlackCurl(string(input))
	if err != nil {
		return err
	}
	if *domain == "" {
		*domain = detectedDomain
	}
	*domain = normalizeWorkspaceDomain(*domain)
	client, err := slack.NewClient(slack.Session{Token: token, Cookie: cookie, UserAgent: userAgent})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("Slack rejected the copied browser request: %w", err)
	}
	if *domain == "" {
		*domain = normalizeWorkspaceDomain(auth.URL)
	}
	if *domain == "" {
		return errors.New("workspace domain was not present in Slack's response; pass --domain <workspace>")
	}
	meta := workspace.Metadata{TeamID: auth.TeamID, Name: auth.Team, Domain: *domain, APIURL: client.APIURL()}
	if err := store.Put(meta, workspace.Secret{Token: token, Cookie: cookie, UserAgent: userAgent}, true); err != nil {
		return err
	}
	fmt.Printf("Added %s (%s)\n", meta.Name, meta.TeamID)
	return nil
}

func curlInput() ([]byte, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return io.ReadAll(io.LimitReader(os.Stdin, 4<<20))
	}
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "pbpaste"
	case "windows":
		command, args = "powershell", []string{"-NoProfile", "-Command", "Get-Clipboard -Raw"}
	default:
		if _, err := exec.LookPath("wl-paste"); err == nil {
			command, args = "wl-paste", []string{"--no-newline"}
		} else {
			command, args = "xclip", []string{"-selection", "clipboard", "-o"}
		}
	}
	data, err := exec.Command(command, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("read copied cURL command from clipboard: %w", err)
	}
	return data, nil
}

func parseSlackCurl(input string) (token, cookie, userAgent, domain string, err error) {
	args, err := shellwords.Parse(strings.TrimSpace(input))
	if err != nil {
		return "", "", "", "", fmt.Errorf("parse copied cURL command: %w", err)
	}
	var data string
	for i := 0; i < len(args); i++ {
		current := args[i]
		if strings.HasPrefix(current, "https://") || strings.HasPrefix(current, "http://") {
			if parsed, parseErr := url.Parse(current); parseErr == nil && strings.HasSuffix(parsed.Hostname(), ".slack.com") {
				domain = strings.TrimSuffix(parsed.Hostname(), ".slack.com")
			}
		}
		if (current == "-H" || current == "--header") && i+1 < len(args) {
			i++
			name, value, ok := strings.Cut(args[i], ":")
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "cookie":
				cookie = cookieValue(value, "d")
			case "user-agent":
				userAgent = strings.TrimSpace(value)
			}
		}
		if (current == "-b" || current == "--cookie") && i+1 < len(args) {
			i++
			cookie = cookieValue(args[i], "d")
		}
		if (current == "--data" || current == "--data-raw" || current == "--data-binary" || current == "-d") && i+1 < len(args) {
			i++
			data = args[i]
		}
	}
	if values, parseErr := url.ParseQuery(data); parseErr == nil {
		token = values.Get("token")
	}
	if token == "" || cookie == "" || userAgent == "" {
		return "", "", "", "", errors.New("the copied request did not contain a token, d cookie, and User-Agent; copy a client.boot request as cURL from Firefox's Network panel")
	}
	return token, cookie, userAgent, domain, nil
}

func cookieValue(header, name string) string {
	for part := range strings.SplitSeq(header, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func diagnoseWorkspace(args []string) error {
	flags := flag.NewFlagSet("workspace diagnose", flag.ContinueOnError)
	domain := flags.String("domain", "", "workspace URL or subdomain")
	saved := flags.Bool("saved", false, "diagnose the active saved session")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *domain == "" && flags.NArg() > 0 {
		*domain = flags.Arg(0)
	}
	var token, rawCookie, userAgent string
	if *saved {
		meta, secret, err := workspace.New(workspace.DefaultPath()).Active()
		if err != nil {
			return err
		}
		*domain, token, rawCookie, userAgent = meta.Domain, secret.Token, secret.Cookie, secret.UserAgent
		fmt.Printf("Saved workspace: name=%q id=%s domain=%s stored_api=%s\n", meta.Name, meta.TeamID, meta.Domain, meta.APIURL)
	} else {
		*domain = normalizeWorkspaceDomain(*domain)
		if *domain == "" {
			return errors.New("usage: slacker workspace diagnose --domain <workspace> or --saved")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("authentication diagnostics require an interactive terminal")
		}
		fmt.Print("xoxc token: ")
		tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		fmt.Print("d/xoxd cookie: ")
		cookieBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return err
		}
		token = strings.TrimSpace(string(tokenBytes))
		rawCookie = strings.TrimSpace(string(cookieBytes))
		userAgent = readUserAgent(bufio.NewReader(os.Stdin))
	}
	if userAgent == "" {
		return errors.New("this session predates browser identity storage; link it again with `slacker workspace add --manual --domain <workspace>` before making another Slack request")
	}
	fmt.Printf("Token: prefix=%t length=%d fingerprint=%s\n", strings.HasPrefix(token, "xoxc-"), len(token), credentialFingerprint(token))
	fmt.Printf("Cookie: prefix=%t length=%d percent_encoded=%t fingerprint=%s\n", strings.HasPrefix(rawCookie, "xoxd-"), len(rawCookie), strings.Contains(rawCookie, "%"), credentialFingerprint(rawCookie))
	fmt.Printf("Browser identity: supplied=%t fingerprint=%s\n", userAgent != "", credentialFingerprint(userAgent))
	client, err := slack.NewClient(slack.Session{Token: token, Cookie: rawCookie, UserAgent: userAgent}, slack.WithRetries(0))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("client.boot failed: %w", err)
	}
	fmt.Printf("PASS client.boot team=%q team_id=%s user=%q user_id=%s\n", auth.Team, auth.TeamID, auth.User, auth.UserID)
	return nil
}

func credentialFingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}

func linkWorkspace(store *workspace.Store, args []string) error {
	flags := flag.NewFlagSet("workspace link", flag.ContinueOnError)
	domain := flags.String("domain", "", "workspace URL or subdomain")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *domain == "" && flags.NArg() > 0 {
		*domain = flags.Arg(0)
	}
	reader := bufio.NewReader(os.Stdin)
	if *domain == "" {
		fmt.Print("Workspace URL (for example, https://example.slack.com): ")
		value, _ := reader.ReadString('\n')
		*domain = value
	}
	*domain = normalizeWorkspaceDomain(*domain)
	if *domain == "" {
		return errors.New("workspace URL or subdomain is required")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("manual linking requires an interactive terminal")
	}
	fmt.Println("In Firefox, open Developer Tools → Storage → Cookies, select slack.com, and copy the value of the cookie named `d`.")
	fmt.Print("Slack d cookie: ")
	cookieBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	cookie := strings.TrimSpace(string(cookieBytes))
	if cookie == "" {
		return errors.New("the Slack d cookie is required")
	}
	userAgent := readUserAgent(reader)
	if userAgent == "" {
		return errors.New("the Firefox user agent is required so Slack sees the same browser identity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	token, err := slack.MintToken(ctx, *domain, cookie, userAgent)
	if err != nil {
		return fmt.Errorf("Slack did not expose a web token for that cookie; use `slacker workspace add --manual --domain %s` to enter both values: %w", *domain, err)
	}
	client, err := slack.NewClient(slack.Session{Token: token, Cookie: cookie, UserAgent: userAgent})
	if err != nil {
		return err
	}
	auth, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("Slack rejected the linked browser session: %w", err)
	}
	meta := workspace.Metadata{TeamID: auth.TeamID, Name: auth.Team, Domain: *domain, APIURL: client.APIURL()}
	if err := store.Put(meta, workspace.Secret{Token: token, Cookie: cookie, UserAgent: userAgent}, true); err != nil {
		return err
	}
	fmt.Printf("Linked %s (%s)\n", meta.Name, meta.TeamID)
	return nil
}

func normalizeWorkspaceDomain(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimSuffix(value, "/")
	value = strings.TrimSuffix(value, ".slack.com")
	return value
}

func readUserAgent(reader *bufio.Reader) string {
	fmt.Println("In the same browser Console, run: navigator.userAgent")
	fmt.Print("Browser user agent: ")
	value, _ := reader.ReadString('\n')
	return strings.TrimSpace(value)
}

func addWorkspace(store *workspace.Store, args []string) error {
	flags := flag.NewFlagSet("workspace add", flag.ContinueOnError)
	all := flags.Bool("all", false, "import every Slack Desktop workspace")
	manual := flags.Bool("manual", false, "enter a browser session manually")
	desktop := flags.Bool("desktop", false, "import an existing Slack Desktop session")
	domain := flags.String("domain", "", "workspace subdomain for manual entry")
	name := flags.String("name", "", "workspace name for manual entry")
	if err := flags.Parse(args); err != nil {
		return err
	}
	selector := ""
	if flags.NArg() > 0 {
		selector = flags.Arg(0)
	}
	if *manual {
		return addManualWorkspace(store, *name, *domain)
	}
	if !*desktop {
		return addBrowserWorkspace(store, selector, *all)
	}
	candidates, err := slackdesktop.Discover()
	if err != nil {
		return fmt.Errorf("discover Slack Desktop sessions: %w", err)
	}
	if len(candidates) > 1 && selector == "" && !*all {
		fmt.Println("Slack Desktop has multiple workspaces:")
		for _, candidate := range candidates {
			fmt.Printf("  %-24s %-18s %s\n", candidate.Workspace.Name, candidate.Workspace.Domain, candidate.Workspace.TeamID)
		}
		return errors.New("choose one with `slacker workspace add <name|domain|team-id>`, or use --all")
	}
	added := 0
	for _, candidate := range candidates {
		ws := candidate.Workspace
		if selector != "" && selector != ws.Name && selector != ws.Domain && selector != ws.TeamID {
			continue
		}
		client, err := slack.NewClient(slack.Session{Token: candidate.Token, Cookie: candidate.Cookie})
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		auth, err := client.Connect(ctx)
		if err != nil {
			if refreshed, mintErr := slack.MintToken(ctx, ws.Domain, candidate.Cookie, ""); mintErr == nil {
				candidate.Token = refreshed
				client, err = slack.NewClient(slack.Session{Token: candidate.Token, Cookie: candidate.Cookie})
				if err == nil {
					auth, err = client.Connect(ctx)
				}
			}
		}
		cancel()
		if err != nil {
			return fmt.Errorf("validate %s: %w", ws.Name, err)
		}
		meta := workspace.Metadata{TeamID: ws.TeamID, Name: ws.Name, Domain: ws.Domain, APIURL: client.APIURL()}
		if auth.TeamID != "" {
			meta.TeamID = auth.TeamID
		}
		if auth.Team != "" {
			meta.Name = auth.Team
		}
		if err := store.Put(meta, workspace.Secret{Token: candidate.Token, Cookie: candidate.Cookie}, added == 0); err != nil {
			return err
		}
		fmt.Printf("Added %s (%s)\n", meta.Name, meta.TeamID)
		added++
		if !*all {
			break
		}
	}
	if added == 0 {
		return fmt.Errorf("no Slack Desktop workspace matched %q", selector)
	}
	return nil
}

func addBrowserWorkspace(store *workspace.Store, selector string, all bool) error {
	fmt.Println("Checking whether Slacker can import the default browser session…")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sessions, err := browserlogin.Login(ctx)
	if err != nil {
		return fmt.Errorf("%w; use `slacker workspace import-curl --domain <workspace>` as the no-profile-access fallback", err)
	}
	added := 0
	for _, session := range sessions {
		if session.UserAgent == "" {
			continue
		}
		client, err := slack.NewClient(slack.Session{Token: session.Token, Cookie: session.Cookie, UserAgent: session.UserAgent})
		if err != nil {
			return err
		}
		validateCtx, validateCancel := context.WithTimeout(context.Background(), 30*time.Second)
		auth, err := client.Connect(validateCtx)
		validateCancel()
		if err != nil {
			continue
		}
		domain := ""
		if parsed, parseErr := url.Parse(auth.URL); parseErr == nil {
			domain = strings.TrimSuffix(parsed.Hostname(), ".slack.com")
		}
		meta := workspace.Metadata{TeamID: session.TeamID, Name: auth.Team, Domain: domain, APIURL: client.APIURL()}
		if auth.TeamID != "" {
			meta.TeamID = auth.TeamID
		}
		if auth.Team != "" {
			meta.Name = auth.Team
		}
		if selector != "" && selector != meta.Name && selector != meta.Domain && selector != meta.TeamID {
			continue
		}
		if err := store.Put(meta, workspace.Secret{Token: session.Token, Cookie: session.Cookie, UserAgent: session.UserAgent}, added == 0); err != nil {
			return err
		}
		fmt.Printf("Added %s (%s)\n", meta.Name, meta.TeamID)
		added++
		if !all {
			break
		}
	}
	if added == 0 {
		if selector != "" {
			return fmt.Errorf("no valid browser workspace matched %q", selector)
		}
		return errors.New("Slack signed in, but no valid browser workspace session was found")
	}
	return nil
}

func addManualWorkspace(store *workspace.Store, name, domain string) error {
	reader := bufio.NewReader(os.Stdin)
	if name == "" {
		fmt.Print("Workspace name: ")
		name, _ = reader.ReadString('\n')
		name = strings.TrimSpace(name)
	}
	if domain == "" {
		fmt.Print("Workspace subdomain: ")
		domain, _ = reader.ReadString('\n')
		domain = normalizeWorkspaceDomain(domain)
	}
	if name == "" || domain == "" {
		return errors.New("workspace name and domain are required")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("manual credential entry requires an interactive terminal")
	}
	fmt.Println("In Firefox on app.slack.com, open Developer Tools → Console and run:")
	fmt.Println(`Object.entries(JSON.parse(localStorage.localConfig_v2).teams).filter(([id]) => id.startsWith("T")).forEach(([id, t]) => console.log(t.name, id, "=>", t.token))`)
	fmt.Print("xoxc token: ")
	tokenBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	fmt.Print("d/xoxd cookie: ")
	cookieBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return err
	}
	userAgent := readUserAgent(reader)
	if userAgent == "" {
		return errors.New("the Firefox user agent is required so Slack sees the same browser identity")
	}
	secret := workspace.Secret{Token: strings.TrimSpace(string(tokenBytes)), Cookie: strings.TrimSpace(string(cookieBytes)), UserAgent: userAgent}
	fmt.Printf("Credential check: token_length=%d token_fingerprint=%s cookie_length=%d cookie_fingerprint=%s\n",
		len(secret.Token), credentialFingerprint(secret.Token), len(secret.Cookie), credentialFingerprint(secret.Cookie))
	client, err := slack.NewClient(slack.Session{Token: secret.Token, Cookie: secret.Cookie, UserAgent: secret.UserAgent})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Validated with client.boot: team=%q team_id=%s user=%q user_id=%s\n", auth.Team, auth.TeamID, auth.User, auth.UserID)
	meta := workspace.Metadata{TeamID: auth.TeamID, Name: auth.Team, Domain: domain, APIURL: client.APIURL()}
	if meta.Name == "" {
		meta.Name = name
	}
	if err := store.Put(meta, secret, true); err != nil {
		return err
	}
	readback, err := store.Secret(meta.TeamID)
	if err != nil {
		return fmt.Errorf("credentials were saved but could not be read back: %w", err)
	}
	fmt.Printf("Keyring read-back: token_match=%t cookie_match=%t token_fingerprint=%s cookie_fingerprint=%s\n",
		readback.Token == secret.Token, readback.Cookie == secret.Cookie,
		credentialFingerprint(readback.Token), credentialFingerprint(readback.Cookie))
	if readback.Token != secret.Token || readback.Cookie != secret.Cookie {
		return errors.New("the operating-system keyring changed the credentials during persistence")
	}
	fmt.Printf("Added %s (%s)\n", meta.Name, meta.TeamID)
	return nil
}

func refreshWorkspace(store *workspace.Store) error {
	meta, secret, err := store.Active()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	token, err := slack.MintToken(ctx, meta.Domain, secret.Cookie, secret.UserAgent)
	if err != nil {
		return err
	}
	secret.Token = token
	client, err := slack.NewClient(slack.Session{Token: token, Cookie: secret.Cookie, CookieS: secret.CookieS, UserAgent: secret.UserAgent})
	if err != nil {
		return err
	}
	auth, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	meta.APIURL = client.APIURL()
	if auth.Team != "" {
		meta.Name = auth.Team
	}
	if err := store.Put(meta, secret, true); err != nil {
		return err
	}
	fmt.Printf("Refreshed %s (%s)\n", meta.Name, meta.TeamID)
	return nil
}

func activeClient() (workspace.Metadata, *slack.Client, error) {
	meta, secret, err := workspace.New(workspace.DefaultPath()).Active()
	if err != nil {
		return workspace.Metadata{}, nil, err
	}
	if secret.UserAgent == "" {
		return workspace.Metadata{}, nil, fmt.Errorf("workspace %s was saved without its browser identity; run `slacker workspace add --manual --domain %s` before making another Slack request", meta.Name, meta.Domain)
	}
	// Always begin on Slack's global API host. Older Slacker builds persisted
	// auth.test's Enterprise Grid navigation URL as an API route, which could
	// validate once during setup and fail on the next launch.
	client, err := slack.NewClient(slack.Session{Token: secret.Token, Cookie: secret.Cookie, CookieS: secret.CookieS, UserAgent: secret.UserAgent})
	return meta, client, err
}

func runDoctor() error {
	meta, client, err := activeClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("workspace %s has an invalid session; run `slacker workspace add %s` to sign in again: %w", meta.Name, meta.TeamID, err)
	}
	fmt.Printf("Workspace: %s (%s)\nUser: %s (%s)\nAPI: %s\nSession: valid\n", meta.Name, auth.TeamID, auth.User, auth.UserID, client.APIURL())
	return nil
}

func runAPI(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: slacker api <method> [key=value ...]")
	}
	_, client, err := activeClient()
	if err != nil {
		return err
	}
	params := url.Values{}
	for _, arg := range args[1:] {
		key, value, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return fmt.Errorf("invalid API parameter %q; expected key=value", arg)
		}
		params.Add(key, value)
	}
	var result map[string]any
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Call(ctx, args[0], params, &result); err != nil {
		return err
	}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	return nil
}

func runSend(selector, message string) error {
	selector = strings.TrimPrefix(strings.TrimSpace(selector), "#")
	message = strings.TrimSpace(message)
	if selector == "" || message == "" {
		return errors.New("usage: slacker --send --channel <name|id> <message>")
	}
	_, client, err := activeClient()
	if err != nil {
		return err
	}
	channelID := selector
	if !strings.HasPrefix(selector, "C") && !strings.HasPrefix(selector, "G") && !strings.HasPrefix(selector, "D") {
		channelID = ""
		cursor := ""
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			conversations, next, listErr := client.ListConversations(ctx, cursor, 200)
			cancel()
			if listErr != nil {
				return listErr
			}
			for _, conversation := range conversations {
				if conversation.Name == selector {
					channelID = conversation.ID
					break
				}
			}
			if channelID != "" || next == "" {
				break
			}
			cursor = next
		}
		if channelID == "" {
			return fmt.Errorf("channel %q was not found", selector)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := client.PostMessage(ctx, channelID, message, ""); err != nil {
		return err
	}
	fmt.Printf("Sent to %s\n", selector)
	return nil
}

func runTUI(cfg *config.Config) error {
	meta, client, err := activeClient()
	if err != nil {
		if !errors.Is(err, workspace.ErrNoActiveWorkspace) {
			return err
		}
		if err := addBrowserWorkspace(workspace.New(workspace.DefaultPath()), "", false); err != nil {
			return err
		}
		meta, client, err = activeClient()
		if err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	auth, connectErr := client.Connect(ctx)
	cancel()
	if connectErr != nil {
		fmt.Printf("The saved session for %s is no longer valid.\n", meta.Name)
		if err := addBrowserWorkspace(workspace.New(workspace.DefaultPath()), meta.TeamID, false); err != nil {
			return fmt.Errorf("sign in again after %v: %w", connectErr, err)
		}
		meta, client, err = activeClient()
		if err != nil {
			return err
		}
		ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		auth, err = client.Connect(ctx)
		if err != nil {
			return err
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conversations, _, err := client.ListConversations(ctx, "", 200)
	if err != nil {
		return err
	}
	sections, _ := client.ListChannelSections(ctx)
	users := map[string]slack.User{}
	if boot, bootErr := client.UserBoot(ctx); bootErr == nil {
		for _, user := range boot.Members {
			users[user.ID] = user
		}
	}
	if _, ok := users[auth.UserID]; !ok {
		users[auth.UserID] = slack.User{ID: auth.UserID, Name: auth.User, RealName: auth.User}
	}
	counts, _ := client.ConversationCounts(ctx)
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	if cfg.Mouse {
		screen.EnableMouse()
	}
	screen.EnablePaste()
	tview.Styles = tview.Theme{}
	return tview.NewApplication(uiroot.NewModel(cfg, meta.Name, auth.UserID, conversations, sections, users, counts, client), tview.WithScreen(screen)).Run()
}

func printUsage() {
	fmt.Println(`slacker — a keyboard-driven Slack terminal client

Usage:
  slacker                         Open the active workspace
  slacker workspace add [name]    Sign in with Slack in a browser
    --desktop                     Import an existing Slack Desktop session
    --manual                      Enter both xoxc token and d cookie
  slacker workspace link [url]    Link using a browser d cookie
  slacker workspace import-curl   Import a copied browser request
  slacker workspace diagnose     Test entered credentials without saving them
    --saved                       Diagnose the active saved session
  slacker workspace list          List configured workspaces
  slacker workspace use <name>    Change the active workspace
  slacker workspace refresh       Refresh the active browser token
  slacker doctor                  Validate the active session
  slacker api <method> [k=v ...]  Call a Slack client API method
  slacker -s -c <channel> <text>  Send a message and exit
  slacker version                 Print the version`)
}
