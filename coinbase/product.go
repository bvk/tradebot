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
	"sort"
	"sync"
	"time"

	"github.com/bvkgo/topic/v2"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/shopspring/decimal"
)

type Product struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	client *Client

	productID string

	tickerCh    <-chan *exchange.Ticker
	tickerTopic *topic.Topic[*exchange.Ticker]
	tickerRecvr *topic.Receiver[*exchange.Ticker]
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
		ctx:         ctx,
		cancel:      cancel,
		client:      c,
		productID:   product,
		tickerTopic: topic.New[*exchange.Ticker](),
	}

	recvr, ch, err := p.tickerTopic.Subscribe(1)
	if err != nil {
		return nil, err
	}
	p.tickerCh = ch
	p.tickerRecvr = recvr

	p.wg.Add(1)
	go p.goWatchPrice()

	// Wait till initial ticker price is received.
	<-p.tickerCh
	return p, nil
}

func (c *Client) CloseProduct(p *Product) error {
	p.cancel(os.ErrClosed)
	p.wg.Wait()
	p.tickerTopic.Close()
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

	var msgs []*MessageType

	var lastSeq int64 = -1
	for ctx.Err() == nil {
		msg, err := ws.NextMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		msgs = append(msgs, msg)
		sort.Slice(msgs, func(i, j int) bool {
			return msgs[i].Sequence < msgs[j].Sequence
		})

		if msgs[0].Sequence != lastSeq+1 {
			if len(msgs) > p.client.opts.MaxWebsocketOutOfOrderAllowance {
				slog.ErrorContext(ctx, "out of order websocket message allowance overflow", "last-seq", lastSeq, "next-msg-seq", msgs[0].Sequence)
				return fmt.Errorf("out of order allowance overflow")
			}
			continue
		}

		msg = msgs[0]
		msgs = slices.Delete(msgs, 0, 1)
		if len(msgs) > 0 {
			slog.Info("resolved an out of order message", "ooo-size", len(msgs))
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
						v := &exchange.Ticker{
							Price:     tick.Price.Decimal,
							Timestamp: exchange.RemoteTime(timestamp),
						}
						p.tickerTopic.SendCh() <- v
					}
				}
			}
		}
	}

	return nil
}

func (p *Product) Price() decimal.Decimal {
	v, _ := topic.Recent(p.tickerTopic)
	return v.Price
}

func (p *Product) Ticker() <-chan *exchange.Ticker {
	_, ch, _ := p.tickerTopic.Subscribe(1)
	return ch
}

func (p *Product) Get(ctx context.Context, serverOrderID exchange.OrderID) (*exchange.Order, error) {
	resp, err := p.client.getOrder(ctx, string(serverOrderID))
	if err != nil {
		return nil, err
	}
	return resp.Order.ToExchangeOrder(), nil
}

func (p *Product) LimitBuy(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (exchange.OrderID, error) {
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
		return "", err
	}
	if !resp.Success {
		slog.ErrorContext(ctx, "create order has failed", "error_response", resp.ErrorResponse)
		return "", errors.New(resp.FailureReason)
	}
	return exchange.OrderID(resp.OrderID), nil
}

func (p *Product) LimitSell(ctx context.Context, clientOrderID string, size, price decimal.Decimal) (exchange.OrderID, error) {
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
		return "", err
	}
	if !resp.Success {
		slog.ErrorContext(ctx, "create order has failed", "error_response", resp.ErrorResponse)
		return "", errors.New(resp.FailureReason)
	}
	return exchange.OrderID(resp.OrderID), nil
}

func (p *Product) Cancel(ctx context.Context, serverOrderID exchange.OrderID) error {
	req := &CancelOrderRequest{
		OrderIDs: []string{string(serverOrderID)},
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
			orders = append(orders, ord.ToExchangeOrder())
		}
	}
	return orders, nil
}

func (p *Product) WaitForDone(ctx context.Context, id exchange.OrderID) error {
	status, timestamp, ok := p.client.orderStatus(string(id))
	if !ok {
		return os.ErrNotExist
	}

	for !slices.Contains(doneStatuses, status) {
		err := p.client.waitForStatusChange(ctx, string(id), timestamp)
		if err != nil {
			return err
		}
		status, timestamp, _ = p.client.orderStatus(string(id))
	}
	return nil
}
