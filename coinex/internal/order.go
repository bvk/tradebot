// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Order struct {
	OrderID          int64           `json:"order_id"`
	ClientID         string          `json:"client_id"`
	Market           string          `json:"market"`
	MarketType       string          `json:"market_type"`
	OrderSide        string          `json:"side"`
	OrderType        string          `json:"type"`
	Currency         string          `json:"ccy"`
	OrderAmount      decimal.Decimal `json:"amount"`
	OrderPrice       decimal.Decimal `json:"price"`
	UnfilledAmount   decimal.Decimal `json:"unfilled_amount"`
	FilledAmount     decimal.Decimal `json:"filled_amount"`
	ExecutedValue    decimal.Decimal `json:"filled_value"`
	LastFilledAmount decimal.Decimal `json:"last_filled_amount"`
	LastFilledPrice  decimal.Decimal `json:"last_filled_price"`
	BaseFee          decimal.Decimal `json:"base_fee"`
	QuoteFee         decimal.Decimal `json:"quote_fee"`
	DiscountFee      decimal.Decimal `json:"discount_fee"`
	MakerFeeRate     decimal.Decimal `json:"maker_fee_rate"`
	TakerFeeRate     decimal.Decimal `json:"taker_fee_rate"`
	CreatedAt        int64           `json:"created_at"`
	UpdatedAt        int64           `json:"updated_at"`
	OrderStatus      string          `json:"status"`

	hasFinishEvent bool `json:"-"`
}

func (v *Order) ServerOrderID() exchange.OrderID {
	return exchange.OrderID(strconv.FormatInt(v.OrderID, 10))
}

func (v *Order) ClientOrderID() string {
	return v.ClientID
}

func (v *Order) Side() string {
	return strings.ToUpper(v.OrderSide)
}

func (v *Order) CreateTime() exchange.RemoteTime {
	return exchange.RemoteTime{Time: time.UnixMilli(v.CreatedAt)}
}

func (v *Order) UpdateTime() exchange.RemoteTime {
	return exchange.RemoteTime{Time: time.UnixMilli(v.UpdatedAt)}
}

func (v *Order) FilledSize() decimal.Decimal {
	return v.FilledAmount
}

func (v *Order) FilledValue() decimal.Decimal {
	return v.ExecutedValue
}

func (v *Order) FilledFee() decimal.Decimal {
	return v.QuoteFee // FIXME: Is this in-sync with BaseFee?
}

func (v *Order) Size() decimal.Decimal {
	return v.OrderAmount
}

func (v *Order) Price() decimal.Decimal {
	return v.OrderPrice
}

func (v *Order) Status() string {
	return v.OrderStatus
}

func (v *Order) AddUpdate(other *Order) error {
	if v.OrderID != other.OrderID {
		return os.ErrInvalid
	}
	if v.ClientID != other.ClientID {
		return os.ErrInvalid
	}
	if v.Market != other.Market {
		return fmt.Errorf("market ids do not match")
	}
	if v.MarketType != other.MarketType {
		return fmt.Errorf("market type ids do not match")
	}
	if v.OrderSide != other.OrderSide {
		return fmt.Errorf("order sides do not match")
	}
	if v.OrderType != other.OrderType {
		return fmt.Errorf("order types do not match")
	}
	if v.Currency != other.Currency {
		return fmt.Errorf("order currencies do not match")
	}
	if v.OrderAmount != other.OrderAmount {
		return fmt.Errorf("order amounts do not match")
	}
	if v.OrderPrice != other.OrderPrice {
		return fmt.Errorf("order prices do not match")
	}
	if v.CreatedAt == 0 && other.CreatedAt != 0 {
		v.CreatedAt = other.CreatedAt
	}
	if v.CreatedAt != 0 && other.CreatedAt != 0 {
		if v.CreatedAt != other.CreatedAt {
			return fmt.Errorf("order create times do not match")
		}
	}
	if v.UnfilledAmount.GreaterThan(other.UnfilledAmount) {
		v.UnfilledAmount = other.UnfilledAmount
	}
	if v.FilledAmount.LessThan(other.FilledAmount) {
		v.FilledAmount = other.FilledAmount
	}
	if v.ExecutedValue.LessThan(other.ExecutedValue) {
		v.ExecutedValue = other.ExecutedValue
	}
	if v.BaseFee.LessThan(other.BaseFee) {
		v.BaseFee = other.BaseFee
	}
	if v.QuoteFee.LessThan(other.QuoteFee) {
		v.QuoteFee = other.QuoteFee
	}
	if v.DiscountFee.LessThan(other.DiscountFee) {
		v.DiscountFee = other.DiscountFee
	}
	if v.UpdatedAt < other.UpdatedAt {
		v.UpdatedAt = other.UpdatedAt
		v.LastFilledAmount = other.LastFilledAmount
		v.LastFilledPrice = other.LastFilledPrice
	}
	if !v.IsDone() && other.IsDone() {
		v.OrderStatus = other.OrderStatus
	}
	if !v.hasFinishEvent && other.hasFinishEvent {
		v.hasFinishEvent = true
	}
	return nil
}

func (v *Order) IsDone() bool {
	if strings.EqualFold(v.OrderStatus, "filled") || strings.EqualFold(v.OrderStatus, "canceled") {
		return true
	}
	return false
}

func (c *Client) goRefreshOrders(ctx context.Context) {
	defer c.wg.Done()

	receiver, err := c.refreshOrdersTopic.Subscribe(0, true)
	if err != nil {
		slog.Error("could not subscribe to refreshOrdersTopic (unexpected)", "err", err)
		return
	}
	defer receiver.Unsubscribe()

	stopf := context.AfterFunc(ctx, receiver.Unsubscribe)
	defer stopf()

	for ctx.Err() == nil {
		order, err := receiver.Receive()
		if err != nil {
			continue
		}

		fresh, err := c.GetOrder(ctx, order.Market, order.OrderID)
		if err != nil {
			if !errors.Is(err, context.Cause(ctx)) {
				slog.Warn("could not query order (will retry)", "market", order.Market, "orderID", order.OrderID, "err", err)
				time.AfterFunc(time.Second, func() {
					c.refreshOrdersTopic.Send(order)
				}) // Schedule a retry.
			}
			continue
		}
		// Publish the order as an update.
		if topic, ok := c.marketOrderUpdateMap.Load(order.Market); ok {
			topic.Send(fresh)
		}
	}
}
