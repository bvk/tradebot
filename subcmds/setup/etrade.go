// Copyright (c) 2026 Deepak Vankadaru

package setup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/xvnc"
	"github.com/visvasity/cli"
	"golang.org/x/term"
)

type ETrade struct {
	dataDir string

	username       string
	consumerKey    string
	consumerSecret string

	sandbox bool
	relogin bool

	browserPath       string
	xvncServerDisplay int
	xvncServerPort    int

	unattended      bool
	extraOutputPath string
}

func (c *ETrade) Purpose() string {
	return "Setup configures E*TRADE API access interactively for automated OAuth authorization later"
}

func (c *ETrade) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("etrade", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.browserPath, "browser-path", "", "path to the chrome browser executable")
	fset.StringVar(&c.username, "username", "", "E*TRADE username")
	fset.StringVar(&c.consumerKey, "consumer-key", "", "E*TRADE application consumer key (falls back to secrets.json if omitted)")
	fset.StringVar(&c.consumerSecret, "consumer-secret", "", "E*TRADE application consumer secret (falls back to secrets.json if omitted)")
	fset.IntVar(&c.xvncServerPort, "xvnc-port", 5900, "When non-zero, VNC server session can be reached at this port")
	fset.IntVar(&c.xvncServerDisplay, "xvnc-display", 0, "When non-zero, browser will be opened in a temp Xvnc session for authentication")
	fset.StringVar(&c.extraOutputPath, "extra-output-path", "", "When non-empty, etrade credentials are also written to this file")
	fset.BoolVar(&c.sandbox, "sandbox", false, "use E*TRADE sandbox environment")
	fset.BoolVar(&c.relogin, "relogin", false, "re-prompt for the E*TRADE website username/password even if already stored")
	fset.BoolVar(&c.unattended, "unattended", false, "When true, automated login must fail when it couldn't proceed without user input")
	return "etrade", fset, cli.CmdFunc(c.run)
}

func (c *ETrade) Description() string {
	return `
Command "etrade" configures E*TRADE API access by guiding the user through
the OAuth 1.0a authorization flow, either manually or with browser automation.

You will need your E*TRADE application consumer key and secret, which are
issued when you register a developer application at developer.etrade.com.

Setup command drives a real chrome browser (via Playwright) through login, any
SMS verification challenge, and the Accept/verifier steps, instead of requiring
you to paste a URL and verifier code by hand:

  $ tradebot setup etrade --username=USER --consumer-key=KEY --consumer-secret=SECRET

The first run prompts once for your E*TRADE website password (masked, never
taken as a flag) and stores them in secrets.json (0600 perms) under --data-dir;
subsequent runs reuse them. The --username, --consumer-key and
--consumer-secret flags can be omitted on later runs -- each falls back to the
value already stored in secrets.json -- but passing either explicitly always
overrides the stored value.

E*TRADE's "remember this device" setting is expected to persist across runs via
the persistent browser profile, so most automated runs should not see an SMS
challenge again after the first; if one does appear, this command pauses and
prompts for the code, which requires the Playwright Chromium browser to be
installed once via:

  $ go run github.com/mxschmitt/playwright-go/cmd/playwright install chromium

IMPORTANT: E*TRADE's login page runs Akamai bot detection. Automation works
most of the time but may not be guaranteed. This is a fraud control on
E*TRADE's side, not a bug to be patched around with fingerprint or behavior
spoofing -- if automation starts failing consistently, fall back to setup
rather than trying to defeat the detection further.

E*TRADE access tokens expire at midnight US Eastern time. So, tradebot service
runs this command in the background with --unattended=true periodically. When
run in the unattended mode, a login failure will pause all E*TRADE jobs and
sends a telegram/pushover notification.
`
}

func (c *ETrade) run(ctx context.Context, args []string) error {
	if len(c.dataDir) == 0 {
		c.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(c.dataDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat data directory %q: %w", c.dataDir, err)
		}
		if err := os.MkdirAll(c.dataDir, 0700); err != nil {
			return fmt.Errorf("could not create data directory %q: %w", c.dataDir, err)
		}
	}
	dataDir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return fmt.Errorf("could not determine data-dir %q absolute path: %w", c.dataDir, err)
	}
	secretsPath := filepath.Join(dataDir, "secrets.json")

	// Collect existing credentials. We should not touch/modify credentials of other services.
	var credentials *autologin.Credentials
	secrets, err := server.SecretsFromFile(secretsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		secrets = new(server.Secrets)
	}
	if secrets.ETrade == nil {
		secrets.ETrade = new(autologin.Credentials)
	}
	if secrets.ETrade.Login == nil {
		secrets.ETrade.Login = new(autologin.LoginCredentials)
	}
	credentials = secrets.ETrade

	// Pick browser path from cmdline or the config in secrets.json file.
	if c.browserPath == "" {
		c.browserPath = credentials.BrowserPath
	}
	credentials.BrowserPath = c.browserPath

	// Check if consumer API keys from secrets.json match with the command-line
	// values. Command-line flags values take priority when present.
	if c.username == "" {
		c.username = credentials.Login.Username
	}
	if c.consumerKey == "" {
		c.consumerKey = credentials.ConsumerKey
	}
	if c.consumerSecret == "" {
		c.consumerSecret = credentials.ConsumerSecret
	}
	if c.username == "" || c.consumerKey == "" || c.consumerSecret == "" {
		return fmt.Errorf("--username, --consumer-key and --consumer-secret are required (no stored values found in secrets.json)")
	}
	if c.username != credentials.Login.Username || c.consumerKey != credentials.ConsumerKey || c.consumerSecret != credentials.ConsumerSecret {
		log.Printf("warning: cmdline consumer API credentials are different from secrets.json credentials (using cmdline credentials)")
	}
	credentials.ConsumerKey = c.consumerKey
	credentials.ConsumerSecret = c.consumerSecret
	credentials.Login.Username = c.username

	// Collect username, password for oauth authentication.
	scanner := bufio.NewScanner(os.Stdin)
	if c.relogin || credentials.Login.Username == "" || credentials.Login.Password == "" {
		newLogin, err := c.promptLoginCredentials(credentials.Login, scanner)
		if err != nil {
			return err
		}
		credentials.Login = newLogin
	}

	if c.xvncServerDisplay != 0 {
		server, err := xvnc.NewServer(c.dataDir, c.xvncServerDisplay)
		if err != nil {
			return err
		}
		defer server.Close()

		var addr *net.TCPAddr
		if c.xvncServerPort != 0 {
			addr = &net.TCPAddr{
				IP:   net.IPv4(0, 0, 0, 0),
				Port: c.xvncServerPort,
			}
		}
		if err := server.Start(addr); err != nil {
			return err
		}
		log.Printf("use vnc-viewer to view the browser (password-file=%s and vnc-server-socket=%s)", server.PasswordFile(), server.ServerSocket())
		os.Setenv("DISPLAY", fmt.Sprintf(":%d", c.xvncServerDisplay))
	}

	// Perform OAuth authentication to obtain the access token.
	accessToken, accessTokenSecret, err := c.getOAuthAccessToken(ctx, credentials.Login, scanner)
	if err != nil {
		return err
	}
	credentials.AccessToken = accessToken
	credentials.AccessTokenSecret = accessTokenSecret

	// Update the account list.
	accounts, err := autologin.OAuthListAccounts(ctx, credentials, c.sandbox)
	if err != nil {
		return fmt.Errorf("could not list accounts: %w", err)
	}
	for i, a := range accounts {
		fmt.Printf("  %d: NAME=%q ID=%s KEY=%s STATUS=%s TYPE=%s\n", i+1, a.AccountName, a.AccountID, a.AccountIDKey, a.Status, a.AccountType)
	}
	accounts = slices.DeleteFunc(accounts, func(a autologin.Account) bool { return a.IsClosed() })
	if len(accounts) == 0 {
		return fmt.Errorf("no trading accounts found for these credentials")
	}
	credentials.Accounts = accounts

	js, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(secretsPath, js, os.FileMode(0600)); err != nil {
		return err
	}
	fmt.Printf("Credentials and OAuth access tokens are saved to %s\n", secretsPath)

	if len(c.extraOutputPath) != 0 {
		js, err := json.MarshalIndent(secrets.ETrade, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(c.extraOutputPath, js, os.FileMode(0600)); err != nil {
			return err
		}
	}
	return nil
}

// getOAuthAccessToken drives a real browser (via the autologin package)
// through login, any SMS verification challenge, and the Accept/verifier
// steps, instead of requiring a human to paste a URL and verifier code.
func (c *ETrade) getOAuthAccessToken(ctx context.Context, login *autologin.LoginCredentials, scanner *bufio.Scanner) (accessToken, accessTokenSecret string, err error) {
	profileDir := filepath.Join(c.dataDir, "etrade", "browser-profile")
	debugDir := filepath.Join(c.dataDir, "etrade", "autologin-debug")

	// Only offer manual-completion help when the browser window is not in
	// unattended mode -- in unattended runs there's no one to see the challenge,
	// so a nil callback here lets Run fail fast with
	// ErrChallengeRequiresManualCompletion instead of hanging on stdin input
	// that will never come.
	var promptForManualCompletion func(context.Context) error
	if !c.unattended {
		promptForManualCompletion = func(context.Context) error {
			log.Println("\nE*TRADE is asking for additional verification (e.g. choosing a phone number and/or entering a code).")
			log.Println("Complete the remaining steps in the browser window, including clicking \"Accept\", then press Enter here to continue.")
			scanner.Scan()
			return nil
		}
	}

	accessToken, accessTokenSecret, err = autologin.Run(ctx, autologin.Options{
		ConsumerKey:               c.consumerKey,
		ConsumerSecret:            c.consumerSecret,
		Login:                     *login,
		Sandbox:                   c.sandbox,
		ExecutablePath:            c.browserPath,
		ProfileDir:                profileDir,
		DebugDir:                  debugDir,
		PromptForManualCompletion: promptForManualCompletion,
	})
	if err != nil {
		return "", "", fmt.Errorf("autologin failed: %w", err)
	}

	return accessToken, accessTokenSecret, nil
}

// promptLoginCredentials interactively reads an E*TRADE website
// username/password. The password is read with terminal echo disabled so it
// never appears on screen, in shell history, or in the process list.
func (c *ETrade) promptLoginCredentials(old *autologin.LoginCredentials, scanner *bufio.Scanner) (*autologin.LoginCredentials, error) {
	login := new(autologin.LoginCredentials)
	if old == nil {
		old = login
	}

	login.Username = old.Username
	if c.relogin || login.Username == "" {
		fmt.Print("E*TRADE username: ")
		if !scanner.Scan() {
			return nil, fmt.Errorf("could not read username")
		}
		username := strings.TrimSpace(scanner.Text())
		if username == "" {
			return nil, fmt.Errorf("username is required")
		}
		login.Username = username
	}

	login.Password = old.Password
	if c.relogin || login.Password == "" {
		fmt.Print("E*TRADE password: ")
		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return nil, fmt.Errorf("could not read password: %w", err)
		}
		login.Password = string(passwordBytes)
	}
	if err := login.Check(); err != nil {
		return nil, err
	}
	return login, nil
}
