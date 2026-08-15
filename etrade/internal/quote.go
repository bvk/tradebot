// Copyright (c) 2026 Deepak Vankadaru

package internal

import (
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

// The following nested types mirror the E*TRADE JSON response structure for
// quotes returned by GET /v1/market/quote/{symbols}. They are unexported and
// used only for unmarshaling. The public Quote type below is derived from them.

type apiQuoteProduct struct {
	Symbol       string `json:"symbol"`
	SecurityType string `json:"securityType"`
}

type apiQuoteAll struct {
	LastTrade decimal.Decimal `json:"lastTrade"`
	Bid       decimal.Decimal `json:"bid"`
	Ask       decimal.Decimal `json:"ask"`

	// Unused: share counts for current best bid/ask.
	BidSize int64 `json:"bidSize"`
	AskSize int64 `json:"askSize"`

	// Unused: intraday price range and session open/close data.
	Open          decimal.Decimal `json:"open"`
	High          decimal.Decimal `json:"high"`
	Low           decimal.Decimal `json:"low"`
	PreviousClose decimal.Decimal `json:"previousClose"`

	// Unused: change from previous close in absolute and percentage terms.
	ChangeClose           decimal.Decimal `json:"changeClose"`
	ChangeClosePercentage decimal.Decimal `json:"changeClosePercentage"`

	// Unused: volume data for the session.
	Volume      int64 `json:"volume"`
	TotalVolume int64 `json:"totalVolume"`

	// Unused: fundamental and valuation metrics.
	MarketCap     decimal.Decimal `json:"marketCap"`
	EPS           decimal.Decimal `json:"eps"`
	PE            decimal.Decimal `json:"pe"`
	Week52High    decimal.Decimal `json:"week52High"`
	Week52Low     decimal.Decimal `json:"week52Low"`
	Beta          decimal.Decimal `json:"beta"`
	DividendYield decimal.Decimal `json:"yield"`

	AdjustedFlag      bool   `json:"adjustedFlag"`
	SharesOutstanding int64  `json:"sharesOutstanding"`
	NextEarningDate   string `json:"nextEarningDate"`

	// Unused: time of the last trade as a formatted string.
	LastTradeTime string `json:"lastTradeTime"`
}

// APIQuoteData is the per-symbol quote entry returned inside QuoteResponse. It
// is exported so that the response struct in client.go can reference it.
type APIQuoteData struct {
	// DateTimeUTC is the quote timestamp as a Unix timestamp in seconds.
	DateTimeUTC int64           `json:"dateTimeUTC"`
	Product     apiQuoteProduct `json:"Product"`
	All         apiQuoteAll     `json:"All"`

	// Unused: whether the quote is a real-time or delayed quote.
	QuoteStatus string `json:"quoteStatus"`
	// Unused: the trading session the quote belongs to, e.g. "REGULAR".
	AhFlag string `json:"ahFlag"`
}

// Quote is a flat representation of an E*TRADE market quote. It implements
// exchange.PriceUpdate.
type Quote struct {
	Symbol      string
	DateTimeUTC int64 // Unix seconds
	LastTrade   decimal.Decimal
	Bid         decimal.Decimal
	Ask         decimal.Decimal

	PE                decimal.Decimal
	MarketCap         decimal.Decimal
	SharesOutstanding int64
	DividendYield     decimal.Decimal
	NextEarningDate   string
	CorpActionFlag    bool
}

var _ exchange.PriceUpdate = &Quote{}

var two = decimal.NewFromInt(2)

// NewQuoteFromAPI converts an E*TRADE APIQuoteData into a flat Quote.
func NewQuoteFromAPI(a *APIQuoteData) *Quote {
	return &Quote{
		Symbol:            a.Product.Symbol,
		DateTimeUTC:       a.DateTimeUTC,
		LastTrade:         a.All.LastTrade,
		Bid:               a.All.Bid,
		Ask:               a.All.Ask,
		MarketCap:         a.All.MarketCap,
		DividendYield:     a.All.DividendYield,
		PE:                a.All.PE,
		SharesOutstanding: a.All.SharesOutstanding,
		NextEarningDate:   a.All.NextEarningDate,
		CorpActionFlag:    a.All.AdjustedFlag,
	}
}

// PricePoint returns the mid-price of the current best bid and ask, which is
// the most accurate real-time price signal for limit order placement. Falls
// back to lastTrade if bid or ask is zero (e.g. outside market hours).
func (q *Quote) PricePoint() (decimal.Decimal, gobs.RemoteTime) {
	var price decimal.Decimal
	if !q.Bid.IsZero() && !q.Ask.IsZero() {
		price = q.Bid.Add(q.Ask).Div(two)
	} else {
		price = q.LastTrade
	}
	return price, gobs.RemoteTime{Time: time.Unix(q.DateTimeUTC, 0)}
}
