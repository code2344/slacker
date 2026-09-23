package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
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
	if err := flags.Parse(args); err != nil {
		return err
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
		return errors.New("usage: slacker workspace <add|list|current|use|remove|refresh>")
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
	default:
		return fmt.Errorf("unknown workspace command %q", args[0])
	}
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
			if refreshed, mintErr := slack.MintToken(ctx, ws.Domain, candidate.Cookie); mintErr == nil {
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
	fmt.Println("Opening Slack sign-in in a browser. Finish signing in there; Slacker will continue automatically.")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	sessions, err := browserlogin.Login(ctx)
	if err != nil {
		return err
	}
	if len(sessions) > 1 && selector == "" && !all {
		fmt.Println("The browser is signed in to multiple workspaces:")
		for _, session := range sessions {
			fmt.Printf("  %-24s %-18s %s\n", session.Name, session.Domain, session.TeamID)
		}
		return errors.New("choose one with `slacker workspace add <name|domain|team-id>`, or use --all")
	}
	added := 0
	for _, session := range sessions {
		if selector != "" && selector != session.Name && selector != session.Domain && selector != session.TeamID {
			continue
		}
		client, err := slack.NewClient(slack.Session{Token: session.Token, Cookie: session.Cookie})
		if err != nil {
			return err
		}
		validateCtx, validateCancel := context.WithTimeout(context.Background(), 30*time.Second)
		auth, err := client.Connect(validateCtx)
		validateCancel()
		if err != nil {
			return fmt.Errorf("Slack signed in, but the browser session was rejected: %w", err)
		}
		meta := workspace.Metadata{TeamID: session.TeamID, Name: session.Name, Domain: session.Domain, APIURL: client.APIURL()}
		if auth.TeamID != "" {
			meta.TeamID = auth.TeamID
		}
		if auth.Team != "" {
			meta.Name = auth.Team
		}
		if err := store.Put(meta, workspace.Secret{Token: session.Token, Cookie: session.Cookie}, added == 0); err != nil {
			return err
		}
		fmt.Printf("Added %s (%s)\n", meta.Name, meta.TeamID)
		added++
		if !all {
			break
		}
	}
	if added == 0 {
		return fmt.Errorf("no browser workspace matched %q", selector)
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
		domain = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(domain, "https://"), ".slack.com"))
	}
	if name == "" || domain == "" {
		return errors.New("workspace name and domain are required")
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("manual credential entry requires an interactive terminal")
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
	secret := workspace.Secret{Token: strings.TrimSpace(string(tokenBytes)), Cookie: strings.TrimSpace(string(cookieBytes))}
	client, err := slack.NewClient(slack.Session{Token: secret.Token, Cookie: secret.Cookie})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	auth, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	meta := workspace.Metadata{TeamID: auth.TeamID, Name: auth.Team, Domain: domain, APIURL: client.APIURL()}
	if meta.Name == "" {
		meta.Name = name
	}
	if err := store.Put(meta, secret, true); err != nil {
		return err
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
	token, err := slack.MintToken(ctx, meta.Domain, secret.Cookie)
	if err != nil {
		return err
	}
	secret.Token = token
	client, err := slack.NewClient(slack.Session{Token: token, Cookie: secret.Cookie, CookieS: secret.CookieS})
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
	client, err := slack.NewClient(slack.Session{Token: secret.Token, Cookie: secret.Cookie, CookieS: secret.CookieS, APIURL: meta.APIURL})
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
	_, connectErr := client.Connect(ctx)
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
		if _, err := client.Connect(ctx); err != nil {
			return err
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conversations, _, err := client.ListConversations(ctx, "", 200)
	if err != nil {
		return err
	}
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
	return tview.NewApplication(uiroot.NewModel(cfg, meta.Name, conversations), tview.WithScreen(screen)).Run()
}

func printUsage() {
	fmt.Println(`slacker — a keyboard-driven Slack terminal client

Usage:
  slacker                         Open the active workspace
  slacker workspace add [name]    Sign in with Slack in a browser
	  --desktop                     Import an existing Slack Desktop session
  slacker workspace list          List configured workspaces
  slacker workspace use <name>    Change the active workspace
	  slacker workspace refresh       Refresh the active browser token
  slacker doctor                  Validate the active session
  slacker api <method> [k=v ...]  Call a Slack client API method
  slacker version                 Print the version`)
}
