// Copyright (c) 2026 Deepak Vankadaru

package autologin

import (
	"fmt"
	"slices"
)

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
