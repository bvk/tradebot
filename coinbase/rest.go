// Copyright (c) 2023 BVK Chaitanya

package coinbase

type ListProductsResponse struct {
	NumProducts int32         `json:"num_products"`
	Products    []ProductType `json:"products"`
}

type ListOrdersResponse struct {
	Orders   []OrderType `json:"orders"`
	Sequence string      `json:"sequence,number"`
	Cursor   string      `json:"cursor"`
	HasNext  bool        `json:"has_next"`
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
	ErrorDetail           string `json"error_details"`
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
