// Copyright (c) 2026 Deepak Vankadaru

package internal

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

// The following nested types mirror the E*TRADE JSON response structure for
// balances returned by GET /v1/accounts/{accountIdKey}/balance. They are
// unexported and used only for unmarshaling. The public Balance type below is
// derived from them.

type apiBalanceComputed struct {
	CashAvailableForInvestment decimal.Decimal `json:"cashAvailableForInvestment"`

	// Unused: settled cash available for withdrawal (excludes unsettled proceeds).
	CashAvailableForWithdrawal decimal.Decimal `json:"cashAvailableForWithdrawal"`

	// Unused: real-time values object (structure varies by account type).
	RealTimeValues json.RawMessage `json:"RealTimeValues"`
}

// APIBalanceResponse is the top-level E*TRADE balance response structure. It
// is exported so that the response struct in client.go can reference it.
type APIBalanceResponse struct {
	// Unused: account identifier key (matches the URL parameter).
	AccountID string `json:"accountId"`

	// Unused: account type, e.g. "INDIVIDUAL", "IRA".
	AccountType string `json:"accountType"`

	// Unused: the institution that holds the account.
	InstitutionType string `json:"institutionType"`

	// Unused: three-letter currency code for the account (always "USD" for US equity accounts).
	Currency string `json:"currency"`

	Computed apiBalanceComputed `json:"Computed"`
}

// Balance is a flat representation of an E*TRADE account balance. It
// implements exchange.BalanceUpdate.
type Balance struct {
	AccountID                  string
	Currency                   string
	CashAvailableForInvestment decimal.Decimal
}

var _ interface {
	Balance() (string, decimal.Decimal)
} = &Balance{}

// NewBalanceFromAPI converts an E*TRADE APIBalanceResponse into a flat Balance.
// For equity accounts, the available funds are the cash available for investing
// from the Computed section, which includes unsettled sale proceeds that
// E*TRADE permits for immediate reuse in new orders.
func NewBalanceFromAPI(a *APIBalanceResponse) *Balance {
	currency := a.Currency
	if currency == "" {
		currency = "USD"
	}
	return &Balance{
		AccountID:                  a.AccountID,
		Currency:                   currency,
		CashAvailableForInvestment: a.Computed.CashAvailableForInvestment,
	}
}

// Balance returns the currency and cash available for placing new orders.
func (b *Balance) Balance() (string, decimal.Decimal) {
	return b.Currency, b.CashAvailableForInvestment
}
