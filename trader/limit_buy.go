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

type LimitBuy struct {
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

func NewLimitBuy(product exchange.Product, taskID string, buyPrice, cancelPrice, size decimal.Decimal) *LimitBuy {
	v := &LimitBuy{
		product:     product,
		taskID:      taskID,
		size:        size,
		price:       buyPrice,
		cancelPrice: cancelPrice,
		tickerCh:    product.TickerCh(),
	}
	return v
}

func (v *LimitBuy) Orders(ctx context.Context) ([]*exchange.Order, error) {
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

func (v *LimitBuy) Run(ctx context.Context) error {
	if v.order != nil {
		return fmt.Errorf("limit-buy was already complete")
	}

	for {
		select {
		case <-ctx.Done():
			if v.orderID != "" {
				slog.Info("cancelling active limit-buy order", "orderID", v.orderID)
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

			if ticker.Price.GreaterThanOrEqual(v.cancelPrice) {
				if v.orderID != "" {
					if err := v.cancel(ctx); err != nil {
						return err
					}
				}
				continue
			}

			if ticker.Price.LessThan(v.cancelPrice) {
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

func (v *LimitBuy) create(ctx context.Context) error {
	clientOrderID := uuid.New().String()
	orderID, err := v.product.LimitBuy(ctx, clientOrderID, v.size, v.price)
	if err != nil {
		slog.ErrorContext(ctx, "could not create order", "error", err)
		return err
	}
	v.orderID = orderID
	v.orderUpdatesCh = v.product.OrderUpdatesCh(v.orderID)
	return nil
}

func (v *LimitBuy) cancel(ctx context.Context) error {
	if err := v.product.Cancel(ctx, v.orderID); err != nil {
		slog.ErrorContext(ctx, "could not cancel order", "error", err)
		return err
	}

	// v.product.Retire(v.orderID)

	v.orderID = ""
	v.orderUpdatesCh = nil
	return nil
}
