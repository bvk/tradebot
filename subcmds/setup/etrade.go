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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bvk/tradebot/etrade"
	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/telegram"
	"github.com/bvkgo/kv/kvmemdb"
	"github.com/visvasity/cli"
	"golang.org/x/term"
)

type ETrade struct {
	dataDir string

	consumerKey    string
	consumerSecret string
	accountID      string

	sandbox     bool
	relogin     bool
	headless    bool
	profileDir  string
	debugDir    string
	browserPath string

	periodic bool
	interval time.Duration
}

func (c *ETrade) Purpose() string {
	return "Setup configures E*TRADE API access interactively for automated OAuth authorization later"
}

func (c *ETrade) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("etrade", flag.ContinueOnError)
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	fset.StringVar(&c.browserPath, "browser-path", "", "path to the chrome browser executable")
	fset.StringVar(&c.consumerKey, "consumer-key", "", "E*TRADE application consumer key (falls back to secrets.json if omitted)")
	fset.StringVar(&c.consumerSecret, "consumer-secret", "", "E*TRADE application consumer secret (falls back to secrets.json if omitted)")
	fset.StringVar(&c.accountID, "account-id", "", "brokerage account number to select (looked up via the API); falls back to the accountIdKey already stored in secrets.json if omitted")
	fset.BoolVar(&c.sandbox, "sandbox", false, "use E*TRADE sandbox environment")
	fset.BoolVar(&c.relogin, "relogin", false, "re-prompt for the E*TRADE website username/password even if already stored")
	fset.BoolVar(&c.headless, "headless", false, "run the browser headless; headless=true is currently blocked by E*TRADE's bot detection -- see Description")
	fset.StringVar(&c.profileDir, "profile-dir", "", "persistent browser profile directory (defaults under --data-dir)")
	fset.StringVar(&c.debugDir, "debug-dir", "", "directory to dump screenshot+HTML on autologin failure (defaults under --data-dir)")
	fset.BoolVar(&c.periodic, "periodic", false, "loop forever, re-running on a schedule instead of once (requires --auto)")
	fset.DurationVar(&c.interval, "interval", 0, "override the schedule with a fixed interval between runs (--periodic only; for testing, e.g. --interval=30s -- not for production use)")
	return "etrade", fset, cli.CmdFunc(c.run)
}

func (c *ETrade) Description() string {
	return `
Command "etrade" configures E*TRADE API access by guiding the user through
the OAuth 1.0a authorization flow, either manually or with browser automation.

You will need your E*TRADE application consumer key and secret, which are
issued when you register a developer application at developer.etrade.com, and
(if you have more than one brokerage account under these credentials) your
account number (--account-id) so the command can look up its accountIdKey via
the API -- this is not the same value as the accountIdKey itself.

Setup command drives a real browser (via Playwright) through login, any SMS
verification challenge, and the Accept/verifier steps, instead of requiring you
to paste a URL and verifier code by hand:

  $ tradebot setup etrade --consumer-key=KEY --consumer-secret=SECRET --account-id=NUMBER

The first run prompts once for your E*TRADE website username/password (masked,
never taken as a flag) and stores them in secrets.json (0600 perms) under
--data-dir; subsequent runs reuse them. --consumer-key and --consumer-secret
may be omitted on later runs -- each falls back to the value already stored in
secrets.json -- but passing either explicitly always overrides the stored
value. --account-id works the same way, except omitting it reuses the
already-resolved accountIdKey directly (no API lookup needed); passing it
explicitly always re-resolves via the API, e.g. to switch accounts.

E*TRADE's "remember this device" setting is expected to persist across runs via
the persistent browser profile, so most automated runs should not see an SMS
challenge again after the first; if one does appear, this command pauses and
prompts for the code, which requires the Playwright Chromium browser to be
installed once via:

  $ go run github.com/mxschmitt/playwright-go/cmd/playwright install chromium

IMPORTANT: E*TRADE's login page runs Akamai bot detection. Testing found
--headless=true (Playwright's default browser mode) is reliably blocked
("status code 942" on the login page); --headless=false (a normal visible
browser window, the default here) works most of the time but is not
guaranteed. This is a fraud control on E*TRADE's side, not a bug to be patched
around with fingerprint or behavior spoofing -- if automation starts failing
consistently, fall back to setup rather than trying to defeat the detection
further.

--periodic=true loops forever instead of running once, re-running the
authorization flow on a schedule -- by default, sleeping until the next ~00:05
US Eastern time between runs, matching when E*TRADE's tokens actually expire:

  $ tradebot setup etrade --periodic

On failure, it sends a Pushover/Telegram alert (if configured in secrets.json)
instead of crashing, and keeps retrying on the next scheduled run rather than
stopping. --periodic-interval overrides the schedule with a fixed wait between
runs -- this is a testing knob only (e.g. --periodic-interval=30s against
sandbox credentials to watch a few cycles without waiting up to 24h), not
something to use in production.

E*TRADE access tokens expire at midnight US Eastern time. Run this command
again each trading day before starting the bot, or use --periodic to automate
that.  `
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
	var credentials *etrade.Credentials
	secrets, err := server.SecretsFromFile(secretsPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		c.relogin = true
		secrets = new(server.Secrets)
	}
	if secrets.ETrade == nil {
		c.relogin = true
		secrets.ETrade = new(autologin.Credentials)
	}
	credentials = secrets.ETrade

	// Check if consumer API keys from secrets.json match with the command-line
	// values. Command-line flags values take priority when present.
	if c.consumerKey == "" {
		c.consumerKey = credentials.ConsumerKey
	}
	if c.consumerSecret == "" {
		c.consumerSecret = credentials.ConsumerSecret
	}
	if c.consumerKey == "" || c.consumerSecret == "" {
		return fmt.Errorf("--consumer-key and --consumer-secret are required (no stored values found in secrets.json)")
	}
	if c.consumerKey != credentials.ConsumerKey || c.consumerSecret != credentials.ConsumerSecret {
		log.Printf("warning: cmdline consumer API credentials are different from secrets.json credentials (using cmdline credentials)")
	}
	credentials.ConsumerKey = c.consumerKey
	credentials.ConsumerSecret = c.consumerSecret

	// Collect username, password for oauth authentication.
	scanner := bufio.NewScanner(os.Stdin)
	if c.relogin || credentials.Login == nil {
		fmt.Printf("Enter your E*TRADE website login (will be stored in %s):\n", secretsPath)
		newLogin, err := c.promptLoginCredentials(credentials.Login, scanner)
		if err != nil {
			return err
		}
		credentials.Login = newLogin
	}

	// Perform OAuth authentication to obtain the access token.
	accessToken, accessTokenSecret, err := c.headlessOAuthAccessToken(ctx, credentials.Login, scanner)
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
	return nil
}

// // runPeriodic loops runOnce forever, sleeping between runs (see
// // sleepUntilNextRun), until ctx is cancelled. Failures are alerted via
// // Pushover/Telegram (if configured) and retried on the next scheduled run
// // rather than stopping the loop.
// func (c *ETrade) runPeriodic(ctx context.Context, dataDir, secretsPath string, scanner *bufio.Scanner) error {
// 	fmt.Println("Running --periodic: re-authorizing on a schedule until interrupted.")
// 	for {
// 		if err := c.runOnce(ctx, scanner); err != nil {
// 			fmt.Fprintf(os.Stderr, "etrade auto-login failed: %v\n", err)
// 			alertOnFailure(ctx, secretsPath, fmt.Sprintf("E*TRADE auto-login failed: %v", err))
// 		} else {
// 			fmt.Println("Auto-login succeeded.")
// 		}
// 		if err := sleepUntilNextRun(ctx, c.interval); err != nil {
// 			return err
// 		}
// 	}
// }

// alertOnFailure best-effort sends a failure notification via Pushover
// and/or Telegram, if configured in secrets.json. It never returns an
// error -- a broken alert channel shouldn't stop the periodic loop.
func alertOnFailure(ctx context.Context, secretsPath, msg string) {
	secrets, err := server.SecretsFromFile(secretsPath)
	if err != nil {
		return
	}
	if secrets.Pushover != nil {
		if client, cerr := pushover.New(secrets.Pushover); cerr == nil {
			if serr := client.SendMessage(ctx, time.Now(), msg); serr != nil {
				fmt.Fprintf(os.Stderr, "could not send pushover alert: %v\n", serr)
			}
		}
	}
	if secrets.Telegram != nil {
		if client, cerr := telegram.New(ctx, kvmemdb.New(), secrets.Telegram); cerr == nil {
			if serr := client.SendMessage(ctx, time.Now(), msg); serr != nil {
				fmt.Fprintf(os.Stderr, "could not send telegram alert: %v\n", serr)
			}
			client.Close()
		}
	}
}

// sleepUntilNextRun blocks until the next scheduled --periodic run. If
// interval > 0, it sleeps exactly that long (a testing knob to avoid
// waiting up to 24h for the real schedule). Otherwise it sleeps until the
// next ~00:05 America/New_York, mirroring etrade/product.go's
// sleepUntilExtendedHoursOpen pattern -- E*TRADE tokens expire at midnight
// ET, so that's when a fresh one is needed.
func sleepUntilNextRun(ctx context.Context, interval time.Duration) error {
	if interval > 0 {
		select {
		case <-time.After(interval):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("could not load NY timezone: %w", err)
	}
	now := time.Now().In(loc)
	year, month, day := now.Date()
	next := time.Date(year, month, day, 0, 5, 0, 0, loc)
	if !now.Before(next) {
		next = next.Add(24 * time.Hour)
	}
	wait := next.Sub(now)
	fmt.Printf("Sleeping until %s (%s away)...\n", next.Format(time.RFC1123), wait.Round(time.Second))
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// // runOnce runs the full manual or --auto authorization flow once and saves
// // the result to secrets.json.
// func (c *ETrade) runOnce(ctx context.Context, creds *autologin.Credentials, scanner *bufio.Scanner) error {
// 	// Obtaining the access token drives a browser.
// 	accessToken, accessTokenSecret, err := c.autoObtainAccessToken(ctx, c.dataDir, creds.ConsumerKey, creds.ConsumerSecret, scanner)
// 	if err != nil {
// 		return err
// 	}

// 	creds.AccessToken = accessToken
// 	creds.AccessTokenSecret = accessTokenSecret

// 	js, err := json.MarshalIndent(secrets, "", "  ")
// 	if err != nil {
// 		return err
// 	}
// 	if err := os.WriteFile(secretsPath, js, os.FileMode(0600)); err != nil {
// 		return err
// 	}
// 	fmt.Printf("\nCredentials saved to %s\n", secretsPath)
// 	return nil
// }

// // resolveConsumerCredentials returns the consumer key/secret to use: the
// // --consumer-key/--consumer-secret flags if given, otherwise whatever is
// // already stored in secrets.json. Errors if neither source has a value.
// func (c *ETrade) resolveConsumerCredentials(existing *etrade.Credentials) (key, secret string, err error) {
// 	key, secret = c.consumerKey, c.consumerSecret
// 	if existing != nil {
// 		if key == "" {
// 			key = existing.ConsumerKey
// 		}
// 		if secret == "" {
// 			secret = existing.ConsumerSecret
// 		}
// 	}
// 	if key == "" {
// 		return "", "", fmt.Errorf("--consumer-key flag is required (no stored value found in secrets.json)")
// 	}
// 	if secret == "" {
// 		return "", "", fmt.Errorf("--consumer-secret flag is required (no stored value found in secrets.json)")
// 	}
// 	return key, secret, nil
// }

// // manualObtainAccessToken is today's original flow: fetch a request token,
// // print the authorize URL, and prompt for the verifier code pasted back
// // after the user authorizes in their own browser.
// func (c *ETrade) manualObtainAccessToken(ctx context.Context, consumerKey, consumerSecret string, scanner *bufio.Scanner) (accessToken, accessTokenSecret string, err error) {
// 	fmt.Println("Fetching OAuth request token...")
// 	requestToken, requestTokenSecret, err := autologin.OAuthRequestToken(ctx, consumerKey, consumerSecret, c.sandbox)
// 	if err != nil {
// 		return "", "", fmt.Errorf("could not fetch request token: %w", err)
// 	}

// 	authURL := fmt.Sprintf("https://us.etrade.com/e/t/etws/authorize?key=%s&token=%s", consumerKey, requestToken)
// 	fmt.Println("\nOpen the following URL in your browser to authorize access:")
// 	fmt.Println()
// 	fmt.Println(" ", authURL)
// 	fmt.Println()

// 	fmt.Print("Enter the verifier code shown after authorization: ")
// 	if !scanner.Scan() {
// 		return "", "", fmt.Errorf("could not read verifier code")
// 	}
// 	verifier := strings.TrimSpace(scanner.Text())
// 	if verifier == "" {
// 		return "", "", fmt.Errorf("verifier code is required")
// 	}

// 	fmt.Println("\nExchanging verifier for access token...")
// 	accessToken, accessTokenSecret, err = autologin.OAuthAccessToken(ctx,
// 		consumerKey, consumerSecret, requestToken, requestTokenSecret, verifier, c.sandbox)
// 	if err != nil {
// 		return "", "", fmt.Errorf("could not exchange verifier for access token: %w", err)
// 	}
// 	fmt.Println("Access token obtained.")
// 	return accessToken, accessTokenSecret, nil
// }

// headlessOAuthAccessToken drives a real browser (via the autologin package)
// through login, any SMS verification challenge, and the Accept/verifier
// steps, instead of requiring a human to paste a URL and verifier code.
func (c *ETrade) headlessOAuthAccessToken(ctx context.Context, login *autologin.LoginCredentials, scanner *bufio.Scanner) (accessToken, accessTokenSecret string, err error) {
	profileDir := c.profileDir
	if profileDir == "" {
		profileDir = filepath.Join(c.dataDir, "etrade-browser-profile")
	}
	debugDir := c.debugDir
	if debugDir == "" {
		debugDir = filepath.Join(c.dataDir, "etrade-autologin-debug")
	}

	// Only offer manual-completion help when the browser window is actually
	// visible (--headless=false) -- in headless/unattended runs there's no
	// one to see the challenge, so a nil callback here lets Run fail fast
	// with ErrChallengeRequiresManualCompletion instead of hanging on stdin
	// input that will never come.
	var promptForManualCompletion func(context.Context) error
	if !c.headless {
		promptForManualCompletion = func(context.Context) error {
			fmt.Println("\nE*TRADE is asking for additional verification (e.g. choosing a phone number and/or entering a code).")
			fmt.Println("Complete the remaining steps in the browser window, including clicking \"Accept\", then press Enter here to continue.")
			scanner.Scan()
			return nil
		}
	}

	fmt.Println("\nLaunching browser to authorize access...")
	accessToken, accessTokenSecret, err = autologin.Run(ctx, autologin.Options{
		ConsumerKey:               c.consumerKey,
		ConsumerSecret:            c.consumerSecret,
		Login:                     *login,
		Sandbox:                   c.sandbox,
		Headless:                  c.headless,
		ExecutablePath:            c.browserPath,
		ProfileDir:                profileDir,
		DebugDir:                  debugDir,
		PromptForManualCompletion: promptForManualCompletion,
	})
	if err != nil {
		return "", "", fmt.Errorf("autologin failed: %w", err)
	}
	fmt.Println("Access token obtained.")
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

// func promptAccountIDSelection(accounts []autologin.Account, scanner *bufio.Scanner) (int, error) {
// 	if len(accounts) == 0 {
// 		return -1, fmt.Errorf("no trading accounts found for these credentials")
// 	}
// 	if len(accounts) == 1 {
// 		return 0, nil
// 	}
// 	fmt.Println("E*TRADE user has the following accounts")
// 	for i, a := range accounts {
// 		fmt.Printf("  %d: %s (ID=%s STATUS=%s TYPE=%s)\n", i+1, a.AccountName, a.AccountID, a.Status, a.AccountType)
// 	}
// 	fmt.Printf("Select an account [0-%d]: ", len(accounts))
// 	if !scanner.Scan() {
// 		return -1, fmt.Errorf("could not read account selection")
// 	}
// 	input := strings.TrimSpace(scanner.Text())
// 	index, err := strconv.Atoi(input)
// 	if err != nil {
// 		return -1, fmt.Errorf("invalid account number %q (must be [0-%d])", input, len(accounts))
// 	}
// 	return index, nil
// }
