// Copyright (c) 2023 BVK Chaitanya

package internal

import "github.com/bvk/tradebot/exchange"

type Order struct {
	UserID string `json:"user_id"`

	// Possible values: [OPEN, FILLED, CANCELLED, EXPIRED, FAILED,
	// UNKNOWN_ORDER_STATUS]
	Status string `json:"status"`

	OrderID       string `json:"order_id"`
	OrderType     string `json:"order_type"`
	ClientOrderID string `json:"client_order_id"`

	ProductID   string `json:"product_id"`
	ProductType string `json:"product_type"`

	Side         string              `json:"side"`
	CreatedTime  exchange.RemoteTime `json:"created_time"`
	LastFillTime exchange.RemoteTime `json:"last_fill_time"`

	Settled        bool                 `json:"settled"`
	FilledSize     exchange.NullDecimal `json:"filled_size"`
	AvgFilledPrice exchange.NullDecimal `json:"average_filled_price"`
	NumberOfFills  string               `json:"number_of_fills"`
	FilledValue    exchange.NullDecimal `json:"filled_value"`

	Fee       exchange.NullDecimal `json:"fee"`
	TotalFees exchange.NullDecimal `json:"total_fees"`

	RejectReason  string `json:"reject_reason"`
	RejectMessage string `json:"reject_message"`

	IsLiquidation bool   `json:"is_liquidation"`
	PendingCancel bool   `json:"pending_cancel"`
	CancelMessage string `json:"cancel_message"`
}

type GetOrderResponse struct {
	Order *Order `json:"order"`
}

type MarketMarketIOC struct {
	QuoteSize exchange.NullDecimal `json:"quote_size"`
	BaseSize  exchange.NullDecimal `json:"base_size"`
}

type LimitLimitGTC struct {
	BaseSize   exchange.NullDecimal `json:"base_size"`
	LimitPrice exchange.NullDecimal `json:"limit_price"`
	PostOnly   bool                 `json:"post_only"`
}

type LimitLimitGTD struct {
	BaseSize   exchange.NullDecimal `json:"base_size"`
	LimitPrice exchange.NullDecimal `json:"limit_price"`
	PostOnly   bool                 `json:"post_only"`
	EndTime    string               `json:"end_time"`
}

type StopLimitStopLimitGTC struct {
	BaseSize      exchange.NullDecimal `json:"base_size"`
	LimitPrice    exchange.NullDecimal `json:"limit_price"`
	StopPrice     exchange.NullDecimal `json:"stop_price"`
	StopDirection string               `json:"stop_direction"`
}

type StopLimitStopLimitGTD struct {
	BaseSize      exchange.NullDecimal `json:"base_size"`
	LimitPrice    exchange.NullDecimal `json:"limit_price"`
	StopPrice     exchange.NullDecimal `json:"stop_price"`
	StopDirection string               `json:"stop_direction"`
	EndTime       string               `json:"end_time"`
}

type OrderConfig struct {
	MarketIOC    *MarketMarketIOC       `json:"market_market_ioc"`
	LimitGTC     *LimitLimitGTC         `json:"limit_limit_gtc"`
	LimitGTD     *LimitLimitGTD         `json:"limit_limit_gtd"`
	StopLimitGTD *StopLimitStopLimitGTD `json:"stop_limit_stop_limit_gtd"`
	StopLimitGTC *StopLimitStopLimitGTC `json:"stop_limit_stop_limit_gtc"`
}

type CreateOrderRequest struct {
	ClientOrderID string       `json:"client_order_id"`
	ProductID     string       `json:"product_id"`
	Side          string       `json:"side"`
	Order         *OrderConfig `json:"order_configuration"`
}

type CreateOrderResponse struct {
	Success         bool                        `json:"success"`
	SuccessResponse *CreateOrderSuccessResponse `json:"success_response"`

	OrderID     string       `json:"order_id"`
	OrderConfig *OrderConfig `json:"order_configuration"`

	FailureReason string                    `json:"failure_reason"`
	ErrorResponse *CreateOrderErrorResponse `json:"error_response"`
}

type CreateOrderSuccessResponse struct {
	OrderID       string `json:"order_id"`
	ProductID     string `json:"product_id"`
	Side          string `json:"side"`
	ClientOrderID string `json:"client_order_id"`
}

type CreateOrderErrorResponse struct {
	Error                 string `json:"error"`
	Message               string `json:"message"`
	ErrorDetail           string `json:"error_details"`
	PreviewFailureReason  string `json:"preview_failure_reason"`
	NewOrderFailureReason string `json:"new_order_failure_reason"`
}

type Product struct {
	ProductID  string `json:"product_id"`
	Status     string `json:"status"`
	IsDisabled bool   `json:"is_disabled"`

	Price     exchange.NullDecimal `json:"price"`
	BaseName  string               `json:"base_name"`
	QuoteName string               `json:"quote_name"`
	BaseIncr  exchange.NullDecimal `json:"base_increment"`
	QuoteIncr exchange.NullDecimal `json:"quote_increment"`

	QuoteMinSize exchange.NullDecimal `json:"quote_min_size"`
	QuoteMaxSize exchange.NullDecimal `json:"quote_max_size"`
	BaseMinSize  exchange.NullDecimal `json:"base_min_size"`
	BaseMaxSize  exchange.NullDecimal `json:"base_max_size"`
}

type ListProductsResponse struct {
	NumProducts int32      `json:"num_products"`
	Products    []*Product `json:"products"`
}

type GetProductResponse struct {
	ProductID string `json:"product_id"`
	Status    string `json:"status"`

	Price                    exchange.NullDecimal `json:"price"`
	PricePercentageChange24h exchange.NullDecimal `json:"price_percentage_change_24h"`

	Volume24h                 exchange.NullDecimal `json:"volume_24h"`
	VolumePercentageChange24h exchange.NullDecimal `json:"volume_percentage_change_24h"`

	BaseIncrement     exchange.NullDecimal `json:"base_increment"`
	BaseMinSize       exchange.NullDecimal `json:"base_min_size"`
	BaseMaxSize       exchange.NullDecimal `json:"base_max_size"`
	BaseName          string               `json:"base_name"`
	BaseCurrencyID    string               `json:"base_currency_id"`
	BaseDisplaySymbol string               `json:"base_display_symbol"`

	QuoteIncrement     exchange.NullDecimal `json:"quote_increment"`
	QuoteMinSize       exchange.NullDecimal `json:"quote_min_size"`
	QuoteMaxSize       exchange.NullDecimal `json:"quote_max_size"`
	QuoteName          string               `json:"quote_name"`
	QuoteCurrencyID    string               `json:"quote_currency_id"`
	QuoteDisplaySymbol string               `json:"quote_display_symbol"`

	Watched                  bool   `json:"watched"`
	IsDisabled               bool   `json:"is_disabled"`
	New                      bool   `json:"new"`
	CancelOnly               bool   `json:"cancel_only"`
	LimitOnly                bool   `json:"limit_only"`
	PostOnly                 bool   `json:"post_only"`
	TradingDisabled          bool   `json:"trading_disabled"`
	AuctionMode              bool   `json:"auction_mode"`
	ProductType              string `json:"product_type"`
	FcmTradingSessionDetails string `json:"fcm_trading_session_details"`
	MidMarketPrice           string `json:"mid_market_price"`
}

type Candle struct {
	Start  int64                `json:"start,string"`
	Low    exchange.NullDecimal `json:"low"`
	High   exchange.NullDecimal `json:"high"`
	Open   exchange.NullDecimal `json:"open"`
	Close  exchange.NullDecimal `json:"close"`
	Volume exchange.NullDecimal `json:"volume"`
}

type GetProductCandlesResponse struct {
	Candles []*Candle `json:"candles"`
}

type Fill struct {
	EntryID            string               `json:"entry_id"`
	TradeID            string               `json:"trade_id"`
	OrderID            string               `json:"order_id"`
	TradeTime          exchange.RemoteTime  `json:"trade_time"`
	TradeType          string               `json:"trade_type"`
	Price              exchange.NullDecimal `json:"price"`
	Size               exchange.NullDecimal `json:"size"`
	Commission         exchange.NullDecimal `json:"commission"`
	ProductID          string               `json:"product_id"`
	SequenceTimestamp  exchange.RemoteTime  `json:"sequence_timestamp"`
	LiquidityIndicator string               `json:"liquidity_indicator"`
	SizeInQuote        bool                 `json:"size_in_quote"`
	UserID             string               `json:"user_id"`
	Side               string               `json:"side"`
}

type ListFillsResponse struct {
	Fills  []*Fill `json:"fills"`
	Cursor string  `json:"cursor"`
}

type ListOrdersResponse struct {
	Orders   []*Order `json:"orders"`
	Sequence string   `json:"sequence,number"`
	Cursor   string   `json:"cursor"`
	HasNext  bool     `json:"has_next"`
}

type CancelOrderRequest struct {
	OrderIDs []string `json:"order_ids"`
}

type CancelOrderResponse struct {
	Results []CancelOrderResultResponse `json:"results"`
}

type CancelOrderResultResponse struct {
	Success       bool   `json:"success"`
	FailureReason string `json:"failure_reason"`
	OrderID       string `json:"order_id"`
}
