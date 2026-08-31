// Copyright (c) 2026 Deepak Vankadaru

// Package autologin drives the E*TRADE OAuth 1.0a browser login flow
// (login -> MFA -> Accept -> verifier PIN) so the daily re-authorization
// dance doesn't require a human to paste a verifier code by hand. It is
// intentionally isolated from etrade/client.go (the trading path) and only
// reuses the non-browser OAuth calls already in etrade/setup.go.
package autologin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// ErrChallengeRequiresManualCompletion is returned when E*TRADE shows
// anything other than the expected Accept page after login (an identity
// verification / phone-picker step, an SMS code prompt, or some other
// challenge variant) and no PromptForManualCompletion callback was
// supplied. Callers should treat this as an alert-worthy condition rather
// than retrying blindly -- a human needs to actually complete the
// challenge.
var ErrChallengeRequiresManualCompletion = errors.New("autologin: login challenge requires manual completion but no callback was provided")

// Selectors below are best-effort guesses based on visible label/button text
// (Playwright's GetByLabel/GetByRole locators, which are more resilient to
// markup churn than CSS classes). usernameLabel, passwordLabel, and
// acceptButtonName are confirmed against the live E*TRADE pages.
const (
	usernameLabel    = "User ID"
	passwordLabel    = "Password"
	logOnButtonName  = "Log On"
	acceptButtonName = "Accept"

	// verifierInputSelector matches the confirmation page's sole verifier
	// input, confirmed against the live page: <input type="text" value="...">
	// with no label, id, or name attribute to key off of instead.
	verifierInputSelector = "input[type='text']"
)

// Options configures a single autologin run.
type Options struct {
	ConsumerKey, ConsumerSecret string
	Login                       LoginCredentials
	Sandbox                     bool

	// ExecutablePath when non-empty must be absolute path to the chrome binary
	// used to login automatically using playwright.
	ExecutablePath string

	// ProfileDir is a persistent Chromium user-data directory. Reusing the
	// same directory across runs is what lets E*TRADE's "remember this
	// device" state carry over, so most runs never see an MFA challenge.
	ProfileDir string

	// DebugDir, if non-empty, receives a screenshot and the page HTML when a
	// step fails, so failures are debuggable instead of silent.
	DebugDir string

	// PromptForManualCompletion is called when anything other than the
	// expected Accept page shows up after login -- an identity-verification
	// phone picker, an SMS code prompt, or any other challenge variant E*TRADE
	// might show. Rather than automating each variant as it's discovered
	// (fragile, and some variants may never have been seen in testing), this
	// hands the whole remaining flow to a human: with Headless=false, they
	// complete the challenge and click Accept directly in the visible browser
	// window, then the callback should block until they signal it's done
	// (e.g. by waiting for an Enter keypress), at which point Run resumes by
	// scraping the verifier directly. Leave nil for unattended/headless runs;
	// ErrChallengeRequiresManualCompletion is returned instead of blocking on
	// input that will never come.
	PromptForManualCompletion func(ctx context.Context) error
}

// Run drives the full browser OAuth dance and returns a fresh access token
// and secret. It does not touch secrets.json -- callers persist the result.
func Run(ctx context.Context, opts Options) (accessToken, accessTokenSecret string, err error) {
	requestToken, requestTokenSecret, err := OAuthRequestToken(ctx, opts.ConsumerKey, opts.ConsumerSecret, opts.Sandbox)
	if err != nil {
		return "", "", fmt.Errorf("autologin: could not fetch request token: %w", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		return "", "", fmt.Errorf("autologin: could not start playwright: %w", err)
	}
	defer pw.Stop()

	args := []string{
		"--disable-blink-features=AutomationControlled",
		"--disable-infobars",
		"--enable-automation-overrides",
	}
	if opts.PromptForManualCompletion == nil {
		args = append(args,
			"--disable-renderer-backgrounding",
			"--disable-backgrounding-occluded-windows",
			"--disable-background-timer-throttling",
			"--disable-ipc-flooding-protection",
			"--kiosk", // Forces full screen root ownership
			"--start-fullscreen",
			"--no-first-run",        // Prevents welcoming popups that steal focus
			"--start-maximized",     // Ensures the window matches the Xvnc size
			"--window-position=0,0", // Forces alignment with the canvas grid
			"--disable-gpu",         // Bypasses Xvnc GLX initialization checks
			"--disable-software-rasterizer",
			"--use-gl=swiftshader", // Forces pure software rendering loop
			"--disable-dev-shm-usage",
		)
	}

	headless := false // a non-nil value for Headless option is required.
	browserCtx, err := pw.Chromium.LaunchPersistentContext(opts.ProfileDir, playwright.BrowserTypeLaunchPersistentContextOptions{
		Args:           args,
		Headless:       &headless,
		ExecutablePath: &opts.ExecutablePath,
	})
	if err != nil {
		return "", "", fmt.Errorf("autologin: could not launch browser: %w", err)
	}
	defer browserCtx.Close()

	page, err := currentPage(browserCtx)
	if err != nil {
		return "", "", err
	}

	dumpDebug := func(step string) {
		if opts.DebugDir == "" {
			return
		}
		if err := os.MkdirAll(opts.DebugDir, 0700); err != nil {
			return
		}
		if html, herr := page.Content(); herr == nil {
			_ = os.WriteFile(filepath.Join(opts.DebugDir, step+".html"), []byte(html), 0600)
		}
		if shot, serr := page.Screenshot(); serr == nil {
			_ = os.WriteFile(filepath.Join(opts.DebugDir, step+".png"), shot, 0600)
		}
	}

	// The browser-facing authorize page lives on the web UI host, which is
	// distinct from the REST API hosts (etrade.ProductionHostname /
	// etrade.SandboxHostname) used for the OAuth token calls above. This
	// matches subcmds/setup/etrade.go's existing authURL construction exactly
	// -- it does not vary by Sandbox either.
	authURL := fmt.Sprintf("https://us.etrade.com/e/t/etws/authorize?key=%s&token=%s", opts.ConsumerKey, requestToken)
	if _, err := page.Goto(authURL); err != nil {
		dumpDebug("goto")
		return "", "", fmt.Errorf("autologin: could not navigate to authorize page: %w", err)
	}

	// The persistent browser context can already have an active, still-
	// logged-in E*TRADE session from a previous run (observed 2026-07-02):
	// navigating straight to the authorize URL then skips the login form
	// entirely and lands directly on Accept. Only fill the login form if
	// it's actually there.
	usernameVisible, err := isUsernameFieldVisible(page)
	if err != nil {
		dumpDebug("login")
		return "", "", fmt.Errorf("autologin: could not check for login form: %w", err)
	}
	if usernameVisible {
		if err := fillLogin(page, opts.Login); err != nil {
			dumpDebug("login")
			return "", "", err
		}
	}

	// Happy path: "remember this device" already trusts this browser
	// profile, so Accept shows up directly. Otherwise, hand the whole
	// challenge off to a human rather than guessing at its shape.
	acceptVisible, err := isAcceptVisible(page)
	if err != nil {
		dumpDebug("challenge")
		return "", "", fmt.Errorf("autologin: could not check for Accept page: %w", err)
	}
	if !acceptVisible {
		if opts.PromptForManualCompletion == nil {
			dumpDebug("challenge")
			return "", "", ErrChallengeRequiresManualCompletion
		}
		if err := opts.PromptForManualCompletion(ctx); err != nil {
			dumpDebug("challenge")
			return "", "", fmt.Errorf("autologin: manual challenge completion failed: %w", err)
		}
		// The human completes the challenge and clicks Accept themselves,
		// so Run resumes directly at scraping the verifier -- clickAccept
		// is only for the happy path below.
	} else {
		if err := clickAccept(page); err != nil {
			dumpDebug("accept")
			return "", "", err
		}
	}

	verifier, err := scrapeVerifier(page)
	// Always dump the confirmation page once we reach it, regardless of
	// outcome -- the verifier regex is a blind guess over full page content
	// (see scrapeVerifier), so ground truth here is what lets it be fixed
	// against the real markup instead of another round of guessing.
	dumpDebug("confirmation")
	if err != nil {
		return "", "", err
	}

	accessToken, accessTokenSecret, err = OAuthAccessToken(ctx,
		opts.ConsumerKey, opts.ConsumerSecret, requestToken, requestTokenSecret, verifier, opts.Sandbox)
	if err != nil {
		return "", "", fmt.Errorf("autologin: could not exchange verifier %q for access token: %w", verifier, err)
	}
	return accessToken, accessTokenSecret, nil
}

func currentPage(browserCtx playwright.BrowserContext) (playwright.Page, error) {
	if pages := browserCtx.Pages(); len(pages) > 0 {
		return pages[0], nil
	}
	page, err := browserCtx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("autologin: could not open page: %w", err)
	}
	return page, nil
}

// usernameField locates the login form's username textbox. GetByLabel("User
// ID") also matches the "Remember User ID" checkbox (its accessible name
// contains "User ID" as a substring), so the textbox role is needed to
// disambiguate.
func usernameField(page playwright.Page) playwright.Locator {
	return page.GetByRole("textbox", playwright.PageGetByRoleOptions{Name: usernameLabel, Exact: playwright.Bool(true)})
}

// isUsernameFieldVisible reports whether the login form shows up within a
// short wait after navigating to the authorize URL. A persistent browser
// context can already have an active, still-logged-in E*TRADE session from
// a previous run, in which case the login form never appears at all and
// the authorize URL goes straight to Accept -- a timeout here is that
// case, not a failure.
func isUsernameFieldVisible(page playwright.Page) (bool, error) {
	err := usernameField(page).WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(10000),
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, playwright.ErrTimeout) {
		return false, nil
	}
	return false, err
}

func fillLogin(page playwright.Page, creds LoginCredentials) error {
	if err := usernameField(page).Fill(creds.Username); err != nil {
		return fmt.Errorf("autologin: could not fill username: %w", err)
	}
	if err := page.GetByLabel(passwordLabel).Fill(creds.Password); err != nil {
		return fmt.Errorf("autologin: could not fill password: %w", err)
	}
	if err := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: logOnButtonName}).Click(); err != nil {
		return fmt.Errorf("autologin: could not click log on: %w", err)
	}
	return nil
}

// isAcceptVisible reports whether the post-login Accept button shows up
// within a short wait (page navigation after clicking Log On isn't
// instant). When "remember this device" doesn't trust this browser
// profile, E*TRADE shows some other challenge instead (identity-
// verification phone picker, SMS code prompt, or other variants) and
// Accept never appears -- isAcceptVisible returning false is how Run
// detects that, without needing to know which specific challenge it is. A
// timeout is treated as "not visible"; any other error is a real failure.
func isAcceptVisible(page playwright.Page) (bool, error) {
	accept := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: acceptButtonName})
	err := accept.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(10000),
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, playwright.ErrTimeout) {
		return false, nil
	}
	return false, err
}

func clickAccept(page playwright.Page) error {
	if err := page.GetByRole("button", playwright.PageGetByRoleOptions{Name: acceptButtonName}).Click(); err != nil {
		return fmt.Errorf("autologin: could not click accept: %w", err)
	}
	return nil
}

func scrapeVerifier(page playwright.Page) (string, error) {
	// Confirmed against the live confirmation page: it's the sole
	// <input type="text" value="CODE\n"> on the page (no label/id), with a
	// trailing newline baked into the value attribute itself.
	value, err := page.Locator(verifierInputSelector).InputValue()
	if err != nil {
		return "", fmt.Errorf("autologin: could not find verifier code on confirmation page: %w", err)
	}
	verifier := strings.TrimSpace(value)
	if verifier == "" {
		return "", fmt.Errorf("autologin: verifier code on confirmation page is empty")
	}
	return verifier, nil
}
