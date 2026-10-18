// Copyright (c) 2026 Deepak Vankadaru

package autologin

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// accountNameRE is the allowed syntax for an E*TRADE account name. The name is
// used as an identifier ("etrade/<AccountName>") and as a map key on the
// server, so it must be a non-empty run of letters, digits, '_' or '-' — no
// spaces, '/', or other punctuation.
var accountNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// LoginCredentials holds the raw E*TRADE website login (username/password),
// as distinct from the OAuth consumer key/secret or access tokens stored in
// etrade.Credentials. Kept in a separate file from secrets.json so the raw
// login password isn't mixed into the file the trading bot reads regularly.
type LoginCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Credentials holds the OAuth 1.0a keys and account identifier required to
// authenticate with the E*TRADE API. All fields are mandatory; the setup
// etrade subcommand populates them interactively and saves them to secrets.json.
type Credentials struct {
	Login *LoginCredentials `json:"login"`

	Accounts []Account `json:"accounts"`

	// ConsumerKey and ConsumerSecret are the application-level OAuth credentials
	// issued by E*TRADE when you register a developer application.
	ConsumerKey    string `json:"consumer_key"`
	ConsumerSecret string `json:"consumer_secret"`

	// AccessToken and AccessTokenSecret are the user-level OAuth credentials
	// obtained during the OAuth 1.0a authorization flow. They must be renewed
	// daily (E*TRADE tokens expire at midnight US Eastern time).
	AccessToken       string `json:"access_token"`
	AccessTokenSecret string `json:"access_token_secret"`

	// Sandbox selects the E*TRADE sandbox environment when true.
	Sandbox bool `json:"sandbox,omitempty"`

	// BrowserPath when non-empty holds path to the browser that should be used
	// to automate periodic logins.
	BrowserPath string `json:"browser_path"`
}

// Check returns an error if any credential field is empty.
func (c *Credentials) Check() error {
	if c.Login == nil {
		return fmt.Errorf("etrade: login credentials are required")
	}
	if err := c.Login.Check(); err != nil {
		return err
	}
	if c.ConsumerKey == "" {
		return fmt.Errorf("etrade: consumer_key is required")
	}
	if c.ConsumerSecret == "" {
		return fmt.Errorf("etrade: consumer_secret is required")
	}
	if c.AccessToken == "" {
		return fmt.Errorf("etrade: access_token is required")
	}
	if c.AccessTokenSecret == "" {
		return fmt.Errorf("etrade: access_token_secret is required")
	}

	// Validate account names. The account name is used as an identifier
	// ("etrade/<AccountName>") and as a (lowercased) map key on the server, so
	// every active account must have a unique, well-formed name. An empty name,
	// a name containing '/', or two accounts whose names collide
	// case-insensitively (e.g. after one is renamed) would make two distinct
	// brokerage accounts resolve to the same identifier. Closed/inactive
	// accounts are not validated.
	seen := make(map[string]string) // lower(AccountName) -> AccountID
	for i := range c.Accounts {
		a := &c.Accounts[i]
		if a.IsClosed() {
			continue
		}
		if !accountNameRE.MatchString(a.AccountName) {
			return fmt.Errorf("etrade: active account %s has an invalid account name %q (must match %s)", a.AccountID, a.AccountName, accountNameRE.String())
		}
		key := strings.ToLower(a.AccountName)
		if other, ok := seen[key]; ok {
			return fmt.Errorf("etrade: active accounts %s and %s have conflicting account name %q", other, a.AccountID, a.AccountName)
		}
		seen[key] = a.AccountID
	}
	return nil
}

// Check returns an error if any credential field is empty.
func (c *LoginCredentials) Check() error {
	if c.Username == "" {
		return fmt.Errorf("autologin: username is required")
	}
	if c.Password == "" {
		return fmt.Errorf("autologin: password is required")
	}
	return nil
}

func (c *Credentials) Clone() *Credentials {
	n := &Credentials{
		Login: &LoginCredentials{
			Username: c.Login.Username,
			Password: c.Login.Password,
		},
		Accounts:          slices.Clone(c.Accounts),
		ConsumerKey:       c.ConsumerKey,
		ConsumerSecret:    c.ConsumerSecret,
		AccessToken:       c.AccessToken,
		AccessTokenSecret: c.AccessTokenSecret,
		Sandbox:           c.Sandbox,
		BrowserPath:       c.BrowserPath,
	}
	return n
}

// ActiveAccounts returns the subset of accounts that are active (not closed).
// Closed/inactive accounts must not be registered, verified, polled or traded.
func (c *Credentials) ActiveAccounts() []Account {
	var active []Account
	for _, a := range c.Accounts {
		if a.IsClosed() {
			continue
		}
		active = append(active, a)
	}
	return active
}

func (c *Credentials) GetAccountIDKey(accountID string) (string, error) {
	if accountID == "" {
		return "", fmt.Errorf("etrade: account id value is empty")
	}
	for _, a := range c.Accounts {
		if a.AccountID == accountID {
			return a.AccountIDKey, nil
		}
	}
	return "", fmt.Errorf("etrade: account id %s is not found in credentials", accountID)
}
