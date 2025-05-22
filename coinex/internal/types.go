// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

type GetMarketsResponse struct {
	Code int `json:"code"`

	Message string `json:"message"`

	Data []*MarketStatus `json:"data"`
}

type MarketStatus struct {
	Market string `json:"market"`
	Status string `json:"status"`

	IsAMMAvailable              bool `json:"is_amm_available"`
	IsMarginAvailable           bool `json:"is_margin_available"`
	IsPreMarketTradingAvailable bool `json:"is_pre_market_trading_available"`
	IsAPITradingAvailable       bool `json:"is_api_trading_available"`

	MakerFeeRate string `json:"maker_fee_rate"`
	TakerFeeRate string `json:"taker_fee_rate"`

	MinAmount decimal.Decimal `json:"min_amount"` // Min. transaction volume

	BaseCurrency  string `json:"base_ccy"`
	BasePrecision int    `json:"base_ccy_precision"`

	QuoteCurrency  string `json:"quote_ccy"`
	QuotePrecision int    `json:"quote_ccy_precision"`
}

type GetMarketInfoResponse struct {
	Code int `json:"code"`

	Message string `json:"message"`

	Data []*MarketInfo `json:"data"`
}

type MarketInfo struct {
	Market string `json:"market"`

	LastPrice decimal.Decimal `json:"last"`

	OpenPrice  decimal.Decimal `json:"open"`
	ClosePrice decimal.Decimal `json:"close"`

	HighPrice decimal.Decimal `json:"high"`
	LowPrice  decimal.Decimal `json:"low"`

	FilledVolume decimal.Decimal `json:"volume"`
	FilledValue  decimal.Decimal `json:"value"`

	SoldVolume   decimal.Decimal `json:"volume_sell"`
	BoughtVolume decimal.Decimal `json:"volume_buy"`

	TimePeriod int64 `json:"period"`
}

type GetBalancesResponse struct {
	Code int `json:"code"`

	Message string `json:"message"`

	Data []*Balance `json:"data"`
}

type Balance struct {
	Currency string `json:"ccy"`

	Available decimal.Decimal `json:"available"`

	Frozen decimal.Decimal `json:"frozen"`
}

type WebsocketHeader struct {
	ID     *int64  `json:"id"`
	Method *string `json:"method"`
}

func (v *WebsocketHeader) IsRequest() bool {
	return v.ID != nil && v.Method != nil
}

func (v *WebsocketHeader) IsResponse() bool {
	return v.ID != nil && v.Method == nil
}

func (v *WebsocketHeader) IsNotice() bool {
	return v.ID == nil && v.Method != nil
}

type WebsocketResponse struct {
	ID      int64  `json:"id"`
	Code    int64  `json:"code"`
	Message string `json:"message"`

	Data json.RawMessage `json:"data"`
}

type WebsocketRequest struct {
	ID     int64  `json:"id"`
	Method string `json:"method"`

	Params json.RawMessage `json:"params"`
}

type WebsocketNotice struct {
	Method string          `json:"method"`
	Data   json.RawMessage `json:"data"`
}

type websocketCall struct {
	Request  WebsocketRequest
	Response WebsocketResponse

	doneCh chan struct{} `json:"-"`
	status error         `json:"-"`
}

type DealUpdate struct {
	DealID int64 `json:"deal_id"`

	Side   string          `json:"side"`
	Price  decimal.Decimal `json:"price"`
	Amount decimal.Decimal `json:"amount"`

	CreatedAtUnixMilli int64 `json:"created_at"`
}
