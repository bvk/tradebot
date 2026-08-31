// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"fmt"
	"time"

	"github.com/bvk/tradebot/etrade/autologin"
	"github.com/bvk/tradebot/exchange"
)

const (
	ProductionHostname = autologin.ProductionHostname
	SandboxHostname    = autologin.SandboxHostname
)

// Options configures the behavior of the E*TRADE exchange client.
type Options struct {
	// Path to the data directory. ETrade metadata will be saved in
	// "<DataDir>/etrade" subdirectory.
	DataDir string

	// Sandbox selects the E*TRADE sandbox environment (apisb.etrade.com) when
	// true, or the production environment (api.etrade.com) when false.
	Sandbox bool

	// HttpClientTimeout is the timeout for individual HTTP requests.
	// Default: 5s.
	HttpClientTimeout time.Duration

	// TokenRenewalInterval is how often the background goroutine calls
	// GET /oauth/renew_access_token to keep the session alive. E*TRADE tokens
	// expire at midnight US Eastern time, so this should be well under 24h.
	// Default: 90min.
	TokenRenewalInterval time.Duration

	// PollOrdersInterval is how often open orders are polled from the API.
	// Default: 10s.
	PollOrdersInterval time.Duration

	// PollPricesInterval is how often price quotes are polled from the API.
	// Default: 5s.
	PollPricesInterval time.Duration

	// PollBalancesInterval is how often the account balance is polled.
	// Default: 30s.
	PollBalancesInterval time.Duration

	// DisplayRunner when non-nil, holds display server instance, X11 or VNC
	// server where GUI application commands (like the web browser for automated
	// login) can be run. This is required to perform unattened periodic logins
	// with E*TRADE.
	DisplayRunner exchange.DisplayRunner

	// Messenger when non-nil, holds a reference to user notification messaging
	// instance.
	Messenger exchange.Messenger

	// cmdlineToolMode when true, disables background tasks.
	cmdlineToolMode bool
}

// restHostname returns the appropriate E*TRADE REST API hostname based on
// whether sandbox mode is enabled.
func (o *Options) restHostname() string {
	if o.Sandbox {
		return SandboxHostname
	}
	return ProductionHostname
}

func (o *Options) setDefaults() {
	if o.HttpClientTimeout == 0 {
		o.HttpClientTimeout = 5 * time.Second
	}
	if o.TokenRenewalInterval == 0 {
		o.TokenRenewalInterval = 90 * time.Minute
	}
	if o.PollOrdersInterval == 0 {
		o.PollOrdersInterval = 10 * time.Second
	}
	if o.PollPricesInterval == 0 {
		o.PollPricesInterval = 5 * time.Second
	}
	if o.PollBalancesInterval == 0 {
		o.PollBalancesInterval = 30 * time.Second
	}
}

// Check validates the options. All fields have defaults so nothing is
// mandatory here; mandatory values live in Credentials.
func (o *Options) Check() error {
	if len(o.DataDir) == 0 {
		if !o.cmdlineToolMode {
			return fmt.Errorf("data directory cannot be empty")
		}
	}
	return nil
}

func CommandOptions() *Options {
	return &Options{cmdlineToolMode: true}
}
