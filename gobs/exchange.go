// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"time"

	"github.com/shopspring/decimal"
)

type RemoteTime struct {
	time.Time
}

type Order struct {
	ServerOrderID string
	ClientOrderID string
	CreateTime    RemoteTime

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

	CurrencyID string

	Available decimal.Decimal
	Hold      decimal.Decimal
}

type Accounts struct {
	Accounts []*Account
}
