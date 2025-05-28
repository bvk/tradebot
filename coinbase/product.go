// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvkgo/topic"
	"github.com/shopspring/decimal"
)

type Product struct {
	client *internal.Client

	exchange *Exchange

	lastTicker *exchange.SimpleTicker

	prodTickerTopic *topic.Topic[exchange.Ticker]
	prodOrderTopic  *topic.Topic[*exchange.SimpleOrder]

	productData *internal.GetProductResponse

	websocket *internal.Websocket
}

func (ex *Exchange) OpenSpotProduct(ctx context.Context, pid string) (_ exchange.Product, status error) {
	if p, ok := ex.productMap.Load(pid); ok {
		return p, nil
	}

	product, err := ex.client.GetProduct(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("could not get product named %q: %w", pid, err)
	}

	p := &Product{
		client:          ex.client,
		exchange:        ex,
		productData:     product,
		prodTickerTopic: topic.New[exchange.Ticker](),
		prodOrderTopic:  topic.New[*exchange.SimpleOrder](),
		websocket:       ex.client.GetMessages("heartbeats", []string{pid}, ex.dispatchMessage),
	}
	p.websocket.Subscribe("ticker", []string{pid})

	ex.productMap.Store(pid, p)
	return p, nil
}

func (p *Product) Close() error {
	p.exchange.productMap.Delete(p.productData.ProductID)
	p.websocket.Close()
	return nil
}

func (p *Product) ProductID() string {
	return p.productData.ProductID
}

func (p *Product) ExchangeName() string {
	return "coinbase"
}

func (p *Product) BaseMinSize() decimal.Decimal {
	return p.productData.BaseMinSize.Decimal
}

func (p *Product) TickerCh() (<-chan exchange.Ticker, func()) {
	sub, ch, _ := p.prodTickerTopic.Subscribe(1, true /* includeRecent */)
	return ch, sub.Unsubscribe
}

func (p *Product) OrderUpdatesCh() (<-chan *exchange.SimpleOrder, func()) {
	sub, ch, _ := p.prodOrderTopic.Subscribe(0, true /* includeRecent */)
	return ch, sub.Unsubscribe
}

func (p *Product) Get(ctx context.Context, serverOrderID exchange.OrderID) (*exchange.SimpleOrder, error) {
	return p.exchange.GetOrder(ctx, "" /* productID */, serverOrderID)
}

func (p *Product) LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (exchange.OrderID, error) {
	if size.LessThan(p.productData.BaseMinSize.Decimal) {
		return "", fmt.Errorf("min size is %s: %w", p.productData.BaseMinSize.Decimal, os.ErrInvalid)
	}
	if size.GreaterThan(p.productData.BaseMaxSize.Decimal) {
		return "", fmt.Errorf("max size is %s: %w", p.productData.BaseMaxSize.Decimal, os.ErrInvalid)
	}

	// check if this is a retry request for the clientOrderID.
	if order, ok := p.exchange.recreateOldOrder(clientOrderID); ok {
		p.prodOrderTopic.Send(order)
		return order.ServerOrderID, nil
	}

	roundPrice := price.Sub(price.Mod(p.productData.QuoteIncrement.Decimal))

	req := &internal.CreateOrderRequest{
		ClientOrderID: clientOrderID,
		ProductID:     p.productData.ProductID,
		Side:          "BUY",
		Order: &internal.OrderConfig{
			LimitGTC: &internal.LimitLimitGTC{
				BaseSize:   exchange.NullDecimal{Decimal: size},
				LimitPrice: exchange.NullDecimal{Decimal: roundPrice},
			},
		},
	}
	resp, err := p.exchange.createReadyOrder(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success {
		slog.ErrorContext(ctx, "create order has failed", "error_response", resp.ErrorResponse)
		return "", errors.New(resp.FailureReason)
	}
	return exchange.OrderID(resp.OrderID), nil
}

func (p *Product) LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (exchange.OrderID, error) {
	if size.LessThan(p.productData.BaseMinSize.Decimal) {
		return "", fmt.Errorf("min size is %s: %w", p.productData.BaseMinSize.Decimal, os.ErrInvalid)
	}
	if size.GreaterThan(p.productData.BaseMaxSize.Decimal) {
		return "", fmt.Errorf("max size is %s: %w", p.productData.BaseMaxSize.Decimal, os.ErrInvalid)
	}

	// check if this is a retry request for the clientOrderID.
	if order, ok := p.exchange.recreateOldOrder(clientOrderID); ok {
		p.prodOrderTopic.Send(order)
		return order.ServerOrderID, nil
	}

	roundPrice := price.Sub(price.Mod(p.productData.QuoteIncrement.Decimal))

	req := &internal.CreateOrderRequest{
		ClientOrderID: clientOrderID,
		ProductID:     p.productData.ProductID,
		Side:          "SELL",
		Order: &internal.OrderConfig{
			LimitGTC: &internal.LimitLimitGTC{
				BaseSize:   exchange.NullDecimal{Decimal: size},
				LimitPrice: exchange.NullDecimal{Decimal: roundPrice},
			},
		},
	}
	resp, err := p.exchange.createReadyOrder(ctx, req)
	if err != nil {
		return "", err
	}
	if !resp.Success {
		slog.ErrorContext(ctx, "create order has failed", "error_response", resp.ErrorResponse)
		return "", errors.New(resp.FailureReason)
	}

	return exchange.OrderID(resp.OrderID), nil
}

func (p *Product) Cancel(ctx context.Context, serverOrderID exchange.OrderID) error {
	req := &internal.CancelOrderRequest{
		OrderIDs: []string{string(serverOrderID)},
	}
	resp, err := p.client.CancelOrder(ctx, req)
	if err != nil {
		return err
	}
	if n := len(resp.Results); n != 1 {
		return fmt.Errorf("unexpected: cancel order response has %d results", n)
	}
	if !resp.Results[0].Success {
		if resp.Results[0].FailureReason != "DUPLICATE_CANCEL_REQUEST" {
			return errors.New(resp.Results[0].FailureReason)
		}
	}
	// Schedule a Get for the canceled order so that a notification is generated.
	var get func(context.Context)
	get = func(ctx context.Context) {
		if _, err := p.exchange.GetOrder(ctx, "" /* productID */, serverOrderID); err != nil {
			log.Printf("could not fetch canceled order %s for notification processing (rescheduled): %v", serverOrderID, err)
			p.client.AfterDurationFunc(time.Second, get)
			return
		}
	}
	p.client.AfterDurationFunc(time.Second, get)
	return nil
}

func (p *Product) handleTickerEvent(timestamp time.Time, event *internal.TickerEvent) {
	if p.lastTicker != nil && timestamp.Before(p.lastTicker.ServerTime.Time) {
		return
	}
	p.lastTicker = &exchange.SimpleTicker{
		ServerTime: exchange.RemoteTime{Time: timestamp},
		Price:      event.Price.Decimal,
	}
	p.prodTickerTopic.Send(p.lastTicker)
}

func (p *Product) handleOrder(order *exchange.SimpleOrder) {
	// We don't want to expose PENDING state outside this package.
	if slices.Contains(readyStatuses, order.Status) {
		p.prodOrderTopic.Send(order)
	}
}
