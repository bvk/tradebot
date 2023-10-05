// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/bvkgo/tradebot/exchange"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type LimitSell struct {
	product exchange.Product

	taskID string

	size decimal.Decimal

	price decimal.Decimal

	cancelPrice decimal.Decimal

	tickerCh <-chan *exchange.Ticker

	orderID exchange.OrderID

	orderUpdatesCh <-chan *exchange.Order

	order *exchange.Order
}

func NewLimitSell(product exchange.Product, taskID string, sellPrice, cancelPrice, size decimal.Decimal) *LimitSell {
	v := &LimitSell{
		product:     product,
		taskID:      taskID,
		size:        size,
		price:       sellPrice,
		cancelPrice: cancelPrice,
		tickerCh:    product.TickerCh(),
	}
	return v
}

func (v *LimitSell) Orders(ctx context.Context) ([]*exchange.Order, error) {
	if v.order == nil {
		return nil, os.ErrInvalid
	}
	if v.order.FilledSize.IsZero() {
		order, err := v.product.Get(ctx, v.orderID)
		if err != nil {
			slog.WarnContext(ctx, "could not fetch final order data (can be retried)", "error", err)
		} else {
			v.order = order
		}
	}
	return []*exchange.Order{v.order}, nil
}

func (v *LimitSell) Run(ctx context.Context) error {
	if v.order != nil {
		return fmt.Errorf("limit sell was already completed")
	}

	for {
		select {
		case <-ctx.Done():
			if v.orderID != "" {
				slog.Info("cancelling active limit-sell order", "orderID", v.orderID)
				if err := v.cancel(context.TODO()); err != nil {
					return err
				}
			}
			return context.Cause(ctx)

		case order := <-v.orderUpdatesCh:
			if order.OrderID != v.orderID {
				slog.ErrorContext(ctx, "unexpected: order update doesn't match the order-id")
				return os.ErrInvalid
			}
			if order.Done {
				if order.DoneReason == "" {
					v.order = order
					return nil
				}
				return fmt.Errorf("order completed with reason: %s", order.DoneReason)
			}

		case ticker := <-v.tickerCh:
			// slog.InfoContext(ctx, "change", "ticker", ticker.Price, "orderID", v.orderID, "updatesCh", v.orderUpdatesCh != nil)

			if ticker.Price.LessThanOrEqual(v.cancelPrice) {
				if v.orderID != "" {
					if err := v.cancel(ctx); err != nil {
						return err
					}
				}
				continue
			}

			if ticker.Price.GreaterThan(v.cancelPrice) {
				if v.orderID == "" {
					if err := v.create(ctx); err != nil {
						return err
					}
				}
				continue
			}
		}
	}
}

func (v *LimitSell) create(ctx context.Context) error {
	clientOrderID := uuid.New().String()
	orderID, err := v.product.LimitSell(ctx, clientOrderID, v.size, v.price)
	if err != nil {
		return err
	}
	v.orderID = orderID
	v.orderUpdatesCh = v.product.OrderUpdatesCh(v.orderID)
	return nil
}

func (v *LimitSell) cancel(ctx context.Context) error {
	if err := v.product.Cancel(ctx, v.orderID); err != nil {
		return err
	}
	// v.product.Retire(v.orderID)
	v.orderID = ""
	v.orderUpdatesCh = nil
	return nil
}
