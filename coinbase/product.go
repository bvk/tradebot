// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/bvkgo/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Product struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	client *Client

	productID string

	tickerCond  sync.Cond
	tickerPrice BigFloat
	tickerTime  time.Time
}

func (c *Client) NewProduct(product string) (_ *Product, status error) {
	if !slices.Contains(c.spotProducts, product) {
		return nil, os.ErrInvalid
	}

	ctx, cancel := context.WithCancelCause(c.ctx)
	defer func() {
		if status != nil {
			cancel(status)
		}
	}()

	p := &Product{
		ctx:       ctx,
		cancel:    cancel,
		client:    c,
		productID: product,
		tickerCond: sync.Cond{
			L: new(sync.Mutex),
		},
	}

	p.wg.Add(1)
	go p.goWatchPrice()
	defer func() {
		if status != nil {
			p.wg.Wait()
		}
	}()

	// Wait till ticker price is received.
	if err := p.waitForPrice(); err != nil {
		return nil, err
	}

	return p, nil
}

func (c *Client) CloseProduct(p *Product) error {
	p.cancel(os.ErrClosed)
	p.wg.Wait()
	return nil
}

func (p *Product) goWatchPrice() {
	defer p.wg.Done()

	for p.ctx.Err() == nil {
		if err := p.watch(p.ctx); err != nil {
			slog.WarnContext(p.ctx, "could not watch for websocket msgs", "error", err)
			if p.ctx.Err() == nil {
				time.Sleep(p.client.opts.WebsocketRetryInterval)
			}
		}
	}
}

func (p *Product) watch(ctx context.Context) (status error) {
	ws, err := p.client.NewWebsocket(ctx, []string{p.productID})
	if err != nil {
		return err
	}
	defer func() {
		if status != nil {
			_ = p.client.CloseWebsocket(ws)
		}
	}()

	if err := ws.Subscribe(ctx, "level2"); err != nil {
		return err
	}
	if err := ws.Subscribe(ctx, "ticker"); err != nil {
		return err
	}

	var lastSeq int64 = -1
	for ctx.Err() == nil {
		msg, err := ws.NextMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		if lastSeq > 0 {
			if msg.Sequence < lastSeq+1 {
				slog.InfoContext(ctx, "out of order websocket message is ignored", "last-seq", lastSeq, "msg-seq", msg.Sequence)
				continue
			}
			if msg.Sequence > lastSeq+1 {
				slog.ErrorContext(ctx, "unexpected sequence; we may've lost a few messages", "last-seq", lastSeq, "msg-seq", msg.Sequence)
				return fmt.Errorf("unexpected sequence number")
			}
		}
		lastSeq = msg.Sequence

		timestamp, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
		if err != nil {
			slog.ErrorContext(ctx, "could not parse websocket msg timestamp", "timestamp", msg.Timestamp)
			return err
		}

		if msg.Channel == "l2_data" {
			// TODO: Update the orderbook.
		}

		if msg.Channel == "ticker" {
			for _, event := range msg.Events {
				for _, tick := range event.Tickers {
					if tick.ProductID == p.productID {
						p.setPrice(tick.Price, timestamp)
					}
				}
			}
		}
	}

	return nil
}

func (p *Product) Price() decimal.Decimal {
	p.tickerCond.L.Lock()
	defer p.tickerCond.L.Unlock()

	return p.tickerPrice.Decimal
}

func (p *Product) setPrice(price BigFloat, at time.Time) {
	p.tickerCond.L.Lock()
	defer p.tickerCond.L.Unlock()

	p.tickerPrice = price
	p.tickerTime = at
	p.tickerCond.Broadcast()
}

func (p *Product) waitForPrice() error {
	p.tickerCond.L.Lock()
	defer p.tickerCond.L.Unlock()

	for p.ctx.Err() == nil && p.tickerPrice.IsZero() {
		p.tickerCond.Wait()
	}
	return p.ctx.Err()
}

func (p *Product) Buy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (*exchange.Order, error) {
	req := &CreateOrderRequest{
		ClientOrderID: clientOrderID,
		ProductID:     p.productID,
		Side:          "BUY",
		Order: OrderConfigType{
			LimitGTC: &LimitLimitGTCType{
				BaseSize:   BigFloat{size},
				LimitPrice: BigFloat{price},
			},
		},
	}
	resp, err := p.client.createOrder(ctx, req)
	if err != nil {
		return nil, err
	}
	status := &exchange.Order{
		ClientOrderID: clientOrderID,
		ServerOrderID: resp.OrderID,
	}
	// TODO: Fill more fields.
	return status, nil
}

func (p *Product) Sell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (*exchange.Order, error) {
	req := &CreateOrderRequest{
		ClientOrderID: clientOrderID,
		ProductID:     p.productID,
		Side:          "SELL",
		Order: OrderConfigType{
			LimitGTC: &LimitLimitGTCType{
				BaseSize:   BigFloat{size},
				LimitPrice: BigFloat{price},
			},
		},
	}
	resp, err := p.client.createOrder(ctx, req)
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		slog.ErrorContext(ctx, "create order has failed", "error_response", resp.ErrorResponse)
		return nil, errors.New(resp.FailureReason)
	}
	status := &exchange.Order{
		ClientOrderID: clientOrderID,
		ServerOrderID: resp.OrderID,
	}
	// TODO: Fill more fields.
	return status, nil
}

func (p *Product) Cancel(ctx context.Context, serverOrderID string) error {
	req := &CancelOrderRequest{
		OrderIDs: []string{serverOrderID},
	}
	resp, err := p.client.cancelOrder(ctx, req)
	if err != nil {
		return err
	}
	if n := len(resp.Results); n != 1 {
		return fmt.Errorf("unexpected: cancel order response has %d results", n)
	}
	if !resp.Results[0].Success {
		return errors.New(resp.Results[0].FailureReason)
	}
	return nil
}

func (p *Product) List(ctx context.Context) ([]*exchange.Order, error) {
	values := make(url.Values)
	values.Set("product_id", p.productID)
	values.Set("order_status", "OPEN")

	var responses []*ListOrdersResponse
	response, cont, err := p.client.listOrders(ctx, values)
	if err != nil {
		return nil, err
	}
	responses = append(responses, response)

	for cont != nil {
		response, cont, err = p.client.listOrders(ctx, cont)
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}

	var orders []*exchange.Order
	for _, resp := range responses {
		for _, ord := range resp.Orders {
			at, err := time.Parse(time.RFC3339Nano, ord.CreatedTime)
			if err != nil {
				return nil, err
			}
			order := &exchange.Order{
				ClientOrderID: ord.ClientOrderID,
				ServerOrderID: ord.OrderID,
				CreatedAt:     at,
				// TODO: More fields
			}
			orders = append(orders, order)
		}
	}
	return orders, nil
}
