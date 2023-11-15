// Copyright (c) 2023 BVK Chaitanya

package coinbase

type ListProductsResponse struct {
	NumProducts int32         `json:"num_products"`
	Products    []ProductType `json:"products"`
}

type GetProductResponse struct {
	ProductID string `json:"product_id"`
	Status    string `json:"status"`

	Price                    NullDecimal `json:"price"`
	PricePercentageChange24h NullDecimal `json:"price_percentage_change_24h"`

	Volume24h                 NullDecimal `json:"volume_24h"`
	VolumePercentageChange24h NullDecimal `json:"volume_percentage_change_24h"`

	BaseIncrement     NullDecimal `json:"base_increment"`
	BaseMinSize       NullDecimal `json:"base_min_size"`
	BaseMaxSize       NullDecimal `json:"base_max_size"`
	BaseName          string      `json:"base_name"`
	BaseCurrencyID    string      `json:"base_currency_id"`
	BaseDisplaySymbol string      `json:"base_display_symbol"`

	QuoteIncrement     NullDecimal `json:"quote_increment"`
	QuoteMinSize       NullDecimal `json:"quote_min_size"`
	QuoteMaxSize       NullDecimal `json:"quote_max_size"`
	QuoteName          string      `json:"quote_name"`
	QuoteCurrencyID    string      `json:"quote_currency_id"`
	QuoteDisplaySymbol string      `json:"quote_display_symbol"`

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

type GetProductCandlesResponse struct {
	Candles []*CandleType `json:"candles"`
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
