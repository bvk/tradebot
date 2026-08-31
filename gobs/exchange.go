// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type RemoteTime struct {
	time.Time
}

type TaxLotID string

const (
	ETradeTaxLotIDPrefix = "https://api.etrade.com/v1/accounts/"
)

func (v TaxLotID) Equal(other TaxLotID) bool {
	if v == other {
		return true
	}
	if strings.HasPrefix(string(v), ETradeTaxLotIDPrefix) &&
		strings.HasPrefix(string(other), ETradeTaxLotIDPrefix) {
		a, err1 := url.Parse(string(v))
		b, err2 := url.Parse(string(other))
		if err1 == nil && err2 == nil {
			aq, bq := a.Query(), b.Query()
			if aq != nil && bq != nil {
				return a.Scheme == b.Scheme &&
					a.Host == b.Host && a.Path == b.Path &&
					aq.Get("Account") == bq.Get("Account") &&
					aq.Get("Symbol") == bq.Get("Symbol") &&
					aq.Get("OrderNo") == bq.Get("OrderNo")
			}
		}
	}
	return false
}

type Order struct {
	ServerOrderID string
	ClientOrderID string

	CreateTime RemoteTime
	FinishTime RemoteTime

	Side   string
	Status string

	FilledFee   decimal.Decimal
	FilledSize  decimal.Decimal
	FilledPrice decimal.Decimal

	Done       bool
	DoneReason string
}

type Candle struct {
	StartTime RemoteTime
	Duration  time.Duration

	Low  decimal.Decimal
	High decimal.Decimal

	Open  decimal.Decimal
	Close decimal.Decimal

	Volume decimal.Decimal
}

type Candles struct {
	Candles []*Candle
}

type Product struct {
	ProductID string
	Status    string

	Price decimal.Decimal

	BaseName          string
	BaseCurrencyID    string
	BaseDisplaySymbol string
	BaseMinSize       decimal.Decimal
	BaseMaxSize       decimal.Decimal
	BaseIncrement     decimal.Decimal

	QuoteName          string
	QuoteCurrencyID    string
	QuoteDisplaySymbol string
	QuoteMinSize       decimal.Decimal
	QuoteMaxSize       decimal.Decimal
	QuoteIncrement     decimal.Decimal
}

type Account struct {
	Timestamp time.Time

	Name       string
	CurrencyID string

	Available decimal.Decimal
	Hold      decimal.Decimal
}

type Accounts struct {
	Accounts []*Account
}
