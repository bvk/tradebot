// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "github.com/shopspring/decimal"

type ListProductsResponse struct {
	NumProducts int32         `json:"num_products"`
	Products    []ProductType `json:"products"`
}

type GetProductResponse struct {
	ProductId                 string          `json:"product_id,omitempty"`
	Price                     decimal.Decimal `json:"price,omitempty,string"`
	PricePercentageChange24h  decimal.Decimal `json:"price_percentage_change_24h,omitempty,string"`
	Volume24h                 decimal.Decimal `json:"volume_24h,omitempty,string"`
	VolumePercentageChange24h decimal.Decimal `json:"volume_percentage_change_24h,omitempty,string"`
	BaseIncrement             decimal.Decimal `json:"base_increment,omitempty,string"`
	QuoteIncrement            decimal.Decimal `json:"quote_increment,omitempty,string"`
	QuoteMinSize              decimal.Decimal `json:"quote_min_size,omitempty,string"`
	QuoteMaxSize              decimal.Decimal `json:"quote_max_size,omitempty,string"`
	BaseMinSize               decimal.Decimal `json:"base_min_size,omitempty,string"`
	BaseMaxSize               decimal.Decimal `json:"base_max_size,omitempty,string"`
	BaseName                  string          `json:"base_name,omitempty"`
	QuoteName                 string          `json:"quote_name,omitempty"`
	Watched                   bool            `json:"watched,omitempty"`
	IsDisabled                bool            `json:"is_disabled,omitempty"`
	New                       bool            `json:"new,omitempty"`
	Status                    string          `json:"status,omitempty"`
	CancelOnly                bool            `json:"cancel_only,omitempty"`
	LimitOnly                 bool            `json:"limit_only,omitempty"`
	PostOnly                  bool            `json:"post_only,omitempty"`
	TradingDisabled           bool            `json:"trading_disabled,omitempty"`
	AuctionMode               bool            `json:"auction_mode,omitempty"`
	ProductType               string          `json:"product_type,omitempty"`
	QuoteCurrencyId           string          `json:"quote_currency_id,omitempty"`
	BaseCurrencyId            string          `json:"base_currency_id,omitempty"`
	FcmTradingSessionDetails  string          `json:"fcm_trading_session_details,omitempty"`
	MidMarketPrice            string          `json:"mid_market_price,omitempty"`
	BaseDisplaySymbol         string          `json:"base_display_symbol,omitempty"`
	QuoteDisplaySymbol        string          `json:"quote_display_symbol,omitempty"`
}

type ListOrdersResponse struct {
	Orders   []*OrderType `json:"orders"`
	Sequence string       `json:"sequence,number"`
	Cursor   string       `json:"cursor"`
	HasNext  bool         `json:"has_next"`
}

type GetOrderResponse struct {
	Order OrderType `json:"order"`
}

type CreateOrderRequest struct {
	ClientOrderID string          `json:"client_order_id"`
	ProductID     string          `json:"product_id"`
	Side          string          `json:"side"`
	Order         OrderConfigType `json:"order_configuration"`
}

type CreateOrderResponse struct {
	Success         bool                        `json:"success"`
	SuccessResponse *CreateOrderSuccessResponse `json:"success_response"`

	OrderID     string           `json:"order_id"`
	OrderConfig *OrderConfigType `json:"order_configuration"`

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
