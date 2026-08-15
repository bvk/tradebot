// Copyright (c) 2026 Deepak Vankadaru

package autologin

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// ProductionHostname is the E*TRADE REST API hostname for live trading.
	ProductionHostname = "api.etrade.com"

	// SandboxHostname is the E*TRADE REST API hostname for sandbox/testing.
	SandboxHostname = "apisb.etrade.com"
)

// Account holds E*TRADE account information returned by the accounts list
// endpoint. Used during setup to discover the accountIdKey to persist.
type Account struct {
	AccountID    string `json:"account_id"`
	AccountIDKey string `json:"account_id_key"`
	AccountType  string `json:"account_type"`
	AccountName  string `json:"account_name"`
	Status       string `json:"status"`
}

func (a *Account) IsClosed() bool {
	return !strings.EqualFold(a.Status, "ACTIVE")
}

// OauthEscape percent-encodes a string per OAuth 1.0a (RFC 5849 §3.6).
// url.QueryEscape is almost correct but encodes spaces as '+'; OAuth requires '%20'.
func OauthEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// oauthSignSetup constructs an OAuth 1.0a Authorization header without
// requiring a Client. token and tokenSecret may be empty (request_token step).
// extra holds additional OAuth params such as oauth_callback or oauth_verifier.
func oauthSignSetup(method, apiURL, consumerKey, consumerSecret, token, tokenSecret string, extra url.Values) string {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)

	params := url.Values{
		"oauth_consumer_key":     {consumerKey},
		"oauth_nonce":            {nonce},
		"oauth_signature_method": {"HMAC-SHA1"},
		"oauth_timestamp":        {timestamp},
		"oauth_version":          {"1.0"},
	}
	if token != "" {
		params.Set("oauth_token", token)
	}
	for k, vs := range extra {
		params[k] = vs
	}

	// Build normalized parameter string (sorted key=value pairs).
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, OauthEscape(k)+"="+OauthEscape(params.Get(k)))
	}
	sigBase := method + "&" + OauthEscape(apiURL) + "&" + OauthEscape(strings.Join(parts, "&"))
	sigKey := OauthEscape(consumerSecret) + "&" + OauthEscape(tokenSecret)

	mac := hmac.New(sha1.New, []byte(sigKey))
	mac.Write([]byte(sigBase))
	params.Set("oauth_signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	// Assemble Authorization header in a stable order.
	order := []string{
		"oauth_callback", "oauth_consumer_key", "oauth_nonce", "oauth_signature",
		"oauth_signature_method", "oauth_timestamp", "oauth_token", "oauth_verifier", "oauth_version",
	}
	var headerParts []string
	for _, k := range order {
		if v := params.Get(k); v != "" {
			headerParts = append(headerParts, k+`="`+OauthEscape(v)+`"`)
		}
	}
	return "OAuth " + strings.Join(headerParts, ", ")
}

// OAuthRequestToken fetches an OAuth 1.0a request token using only the
// consumer credentials. Returns the short-lived request token and its secret,
// which must be exchanged for access credentials via OAuthAccessToken.
func OAuthRequestToken(ctx context.Context, consumerKey, consumerSecret string, sandbox bool) (token, tokenSecret string, err error) {
	hostname := ProductionHostname
	if sandbox {
		hostname = SandboxHostname
	}
	apiURL := "https://" + hostname + "/oauth/request_token"
	header := oauthSignSetup("GET", apiURL, consumerKey, consumerSecret, "", "", url.Values{"oauth_callback": {"oob"}})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", header)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("etrade: request_token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("etrade: could not read request_token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("etrade: request_token returned HTTP %d: %s", resp.StatusCode, body)
	}

	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", fmt.Errorf("etrade: could not parse request_token response: %w", err)
	}
	token = vals.Get("oauth_token")
	tokenSecret = vals.Get("oauth_token_secret")
	if token == "" || tokenSecret == "" {
		return "", "", fmt.Errorf("etrade: request_token response missing oauth_token or oauth_token_secret")
	}
	return token, tokenSecret, nil
}

// OAuthAccessToken exchanges a request token and verifier code for long-lived
// access credentials. verifier is the code shown in the browser after the user
// authorizes the application at the E*TRADE authorization URL.
func OAuthAccessToken(ctx context.Context, consumerKey, consumerSecret, requestToken, requestTokenSecret, verifier string, sandbox bool) (accessToken, accessTokenSecret string, err error) {
	hostname := ProductionHostname
	if sandbox {
		hostname = SandboxHostname
	}
	apiURL := "https://" + hostname + "/oauth/access_token"
	header := oauthSignSetup("GET", apiURL, consumerKey, consumerSecret,
		requestToken, requestTokenSecret, url.Values{"oauth_verifier": {verifier}})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", header)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("etrade: access_token request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("etrade: could not read access_token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("etrade: access_token returned HTTP %d: %s", resp.StatusCode, body)
	}

	vals, err := url.ParseQuery(string(body))
	if err != nil {
		return "", "", fmt.Errorf("etrade: could not parse access_token response: %w", err)
	}
	accessToken = vals.Get("oauth_token")
	accessTokenSecret = vals.Get("oauth_token_secret")
	if accessToken == "" || accessTokenSecret == "" {
		return "", "", fmt.Errorf("etrade: access_token response missing oauth_token or oauth_token_secret")
	}
	return accessToken, accessTokenSecret, nil
}

// OAuthListAccounts returns the user's E*TRADE accounts using the given
// credentials. Typically called during setup to discover the accountIdKey.
func OAuthListAccounts(ctx context.Context, creds *Credentials, sandbox bool) ([]Account, error) {
	hostname := ProductionHostname
	if sandbox {
		hostname = SandboxHostname
	}
	apiURL := "https://" + hostname + "/v1/accounts/list"
	header := oauthSignSetup("GET", apiURL, creds.ConsumerKey, creds.ConsumerSecret,
		creds.AccessToken, creds.AccessTokenSecret, nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", header)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("etrade: accounts list request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("etrade: could not read accounts list response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("etrade: accounts list returned HTTP %d: %s", resp.StatusCode, body)
	}

	var wrapper struct {
		AccountListResponse struct {
			Accounts struct {
				Account []struct {
					AccountID    string `json:"accountId"`
					AccountIDKey string `json:"accountIdKey"`
					AccountType  string `json:"accountType"`
					AccountName  string `json:"accountName"`
					Status       string `json:"accountStatus"`
				} `json:"Account"`
			} `json:"Accounts"`
		} `json:"AccountListResponse"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, fmt.Errorf("etrade: could not parse accounts list response: %w", err)
	}

	raw := wrapper.AccountListResponse.Accounts.Account
	accounts := make([]Account, len(raw))
	for i, a := range raw {
		accounts[i] = Account{
			AccountID:    a.AccountID,
			AccountIDKey: a.AccountIDKey,
			AccountType:  a.AccountType,
			AccountName:  a.AccountName,
			Status:       a.Status,
		}
	}
	return accounts, nil
}
