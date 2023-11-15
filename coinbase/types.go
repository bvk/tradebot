// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"slices"

	"github.com/bvk/tradebot/exchange"
)

type ProductType struct {
	ProductID  string `json:"product_id"`
	Status     string `json:"status"`
	IsDisabled bool   `json:"is_disabled"`

	Price     NullDecimal `json:"price"`
	BaseName  string      `json:"base_name"`
	QuoteName string      `json:"quote_name"`
	BaseIncr  NullDecimal `json:"base_increment"`
	QuoteIncr NullDecimal `json:"quote_increment"`

	QuoteMinSize NullDecimal `json:"quote_min_size"`
	QuoteMaxSize NullDecimal `json:"quote_max_size"`
	BaseMinSize  NullDecimal `json:"base_min_size"`
	BaseMaxSize  NullDecimal `json:"base_max_size"`
}

type CandleType struct {
	Start  int64       `json:"start,string"`
	Low    NullDecimal `json:"low"`
	High   NullDecimal `json:"high"`
	Open   NullDecimal `json:"open"`
	Close  NullDecimal `json:"close"`
	Volume NullDecimal `json:"volume"`
}

type MessageType struct {
	Type string `json:"type"`

	// Message holds description when Type is "error".
	Message string `json:"message"`

	ProductIDs []string `json:"product_ids"`
	Channel    string   `json:"channel"`
	APIKey     string   `json:"api_key"`
	Timestamp  string   `json:"timestamp"`
	Signature  string   `json:"signature"`

	Sequence int64 `json:"sequence_num,number"`

	ClientID string      `json:"client_id"`
	Events   []EventType `json:"events"`
}

type EventType struct {
	Type      string             `json:"type"`
	ProductID string             `json:"product_id"`
	Updates   []*UpdateEventType `json:"updates"`
	Tickers   []*TickerEventType `json:"tickers"`
	Orders    []*OrderEventType  `json:"orders"`
}

type UpdateEventType struct {
	Side        string      `json:"side"`
	EventTime   string      `json:"event_time"`
	PriceLevel  NullDecimal `json:"price_level"`
	NewQuantity NullDecimal `json:"new_quantity"`
}

type TickerEventType struct {
	Type        string      `json:"type"`
	ProductID   string      `json:"product_id"`
	Price       NullDecimal `json:"price"`
	Volume24H   NullDecimal `json:"volume_24_h"`
	Low24H      NullDecimal `json:"low_24_h"`
	High24H     NullDecimal `json:"high_24_h"`
	Low52W      NullDecimal `json:"low_52_w"`
	High52W     NullDecimal `json:"high_52_w"`
	PricePct24H NullDecimal `json:"price_percent_chg_24_h"`
}

type OrderEventType struct {
	OrderID            string              `json:"order_id"`
	ClientOrderID      string              `json:"client_order_id"`
	Status             string              `json:"status"`
	ProductID          string              `json:"product_id"`
	CreatedTime        exchange.RemoteTime `json:"creation_time"`
	OrderSide          string              `json:"order_side"`
	OrderType          string              `json:"order_type"`
	CancelReason       string              `json:"cancel_reason"`
	RejectReason       string              `json:"reject_reason"`
	CumulativeQuantity NullDecimal         `json:"cumulative_quantity"`
	TotalFees          NullDecimal         `json:"total_fees"`
	AvgPrice           NullDecimal         `json:"avg_price"`
}

type OrderType struct {
	UserID string `json:"user_id"`

	// Possible values: [OPEN, FILLED, CANCELLED, EXPIRED, FAILED,
	// UNKNOWN_ORDER_STATUS]
	Status string `json:"status"`

	OrderID       string `json:"order_id"`
	OrderType     string `json:"order_type"`
	ClientOrderID string `json:"client_order_id"`

	ProductID   string `json:"product_id"`
	ProductType string `json:"product_type"`

	Side        string              `json:"side"`
	CreatedTime exchange.RemoteTime `json:"created_time"`
	Settled     bool                `json:"settled"`

	FilledSize     NullDecimal `json:"filled_size"`
	AvgFilledPrice NullDecimal `json:"average_filled_price"`
	NumberOfFills  string      `json:"number_of_fills"`
	FilledValue    NullDecimal `json:"filled_value"`

	Fee       NullDecimal `json:"fee"`
	TotalFees NullDecimal `json:"total_fees"`

	RejectReason  string `json:"reject_reason"`
	RejectMessage string `json:"reject_message"`

	IsLiquidation bool   `json:"is_liquidation"`
	PendingCancel bool   `json:"pending_cancel"`
	CancelMessage string `json:"cancel_message"`
}

func toExchangeOrder(v *OrderType) *exchange.Order {
	order := &exchange.Order{
		ClientOrderID: v.ClientOrderID,
		OrderID:       exchange.OrderID(v.OrderID),
		CreateTime:    exchange.RemoteTime{Time: v.CreatedTime.Time},
		Side:          v.Side,
		Fee:           v.Fee.Decimal,
		FilledSize:    v.FilledSize.Decimal,
		FilledPrice:   v.AvgFilledPrice.Decimal,
		Status:        v.Status,
		Done:          slices.Contains(doneStatuses, v.Status),
	}
	if order.Done && order.Status != "FILLED" {
		order.DoneReason = order.Status
	}
	return order
}

type MarketMarketIOCType struct {
	QuoteSize NullDecimal `json:"quote_size"`
	BaseSize  NullDecimal `json:"base_size"`
}

type LimitLimitGTCType struct {
	BaseSize   NullDecimal `json:"base_size"`
	LimitPrice NullDecimal `json:"limit_price"`
	PostOnly   bool        `json:"post_only"`
}

type LimitLimitGTDType struct {
	BaseSize   NullDecimal `json:"base_size"`
	LimitPrice NullDecimal `json:"limit_price"`
	PostOnly   bool        `json:"post_only"`
	EndTime    string      `json:"end_time"`
}

type StopLimitStopLimitGTCType struct {
	BaseSize      NullDecimal `json:"base_size"`
	LimitPrice    NullDecimal `json:"limit_price"`
	StopPrice     NullDecimal `json:"stop_price"`
	StopDirection string      `json:"stop_direction"`
}

type StopLimitStopLimitGTDType struct {
	BaseSize      NullDecimal `json:"base_size"`
	LimitPrice    NullDecimal `json:"limit_price"`
	StopPrice     NullDecimal `json:"stop_price"`
	StopDirection string      `json:"stop_direction"`
	EndTime       string      `json:"end_time"`
}

type OrderConfigType struct {
	MarketIOC    *MarketMarketIOCType       `json:"market_market_ioc"`
	LimitGTC     *LimitLimitGTCType         `json:"limit_limit_gtc"`
	LimitGTD     *LimitLimitGTDType         `json:"limit_limit_gtd"`
	StopLimitGTD *StopLimitStopLimitGTDType `json:"stop_limit_stop_limit_gtd"`
	StopLimitGTC *StopLimitStopLimitGTCType `json:"stop_limit_stop_limit_gtc"`
}
