// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
	"github.com/bvkgo/topic"
)

type Exchange struct {
	client *internal.Client

	websocket *internal.Websocket

	// clientOrderIDMap holds client-order-id to exchange.Order mapping for all
	// known orders. TODO: We should cleanup the oldest orders.
	clientOrderIDMap syncmap.Map[string, *exchange.Order]

	productMap syncmap.Map[string, *Product]

	rawOrderTopic *topic.Topic[*internal.Order]

	datastore *Datastore

	// lastFilledTime keeps track of a timestamp before which all completed
	// orders with non-zero filled-size are saved and available in our local
	// datastore. We determine this timestamp by scanning the datastore keys in
	// the constructor.
	lastFilledTime time.Time

	// pendingMap holds a mapping client-order-id to a signal channel. Coinbase
	// orders are created in a non-ready PENDING state and move to ready state
	// after a little while, during which, we cannot use the OrderID with some
	// operations (eg: CancelOrder), so we use this map to make the callers wait
	// till the orders becomes ready.
	pendingMap syncmap.Map[string, chan struct{}]
}

// New creates a client for coinbase exchange.
func New(ctx context.Context, db kv.Database, key, secret string) (_ *Exchange, status error) {
	client, err := internal.New(key, secret, nil /* options */)
	if err != nil {
		return nil, fmt.Errorf("could not create coinbase client: %w", err)
	}
	defer func() {
		if status != nil {
			client.Close()
		}
	}()

	ps, err := client.ListProducts(ctx, "SPOT")
	if err != nil {
		return nil, fmt.Errorf("could not list coinbase products: %w", err)
	}
	pids := make([]string, 0, len(ps.Products))
	for _, p := range ps.Products {
		if strings.HasSuffix(p.ProductID, "-USD") {
			pids = append(pids, p.ProductID)
		}
	}

	exchange := &Exchange{
		client:        client,
		datastore:     NewDatastore(db),
		rawOrderTopic: topic.New[*internal.Order](),
	}
	defer func() {
		if status != nil {
			exchange.rawOrderTopic.Close()
		}
	}()

	// User channel is subscribed for all supported products in a separate
	// connection from product specific channels.
	exchange.websocket = client.GetMessages("heartbeats", pids, exchange.dispatchMessage)
	exchange.websocket.Subscribe("user", pids)

	// Find out the last saved timestamp and fetch all FILLED and CANCELLED
	// orders from that timestamp (with some hours overlap).
	client.Go(exchange.goSaveOrders)
	lastFilledTime, err := exchange.datastore.LastFilledTime(ctx)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not find out last filled time: %w", err)
		}
		lastFilledTime = time.Date(2023, time.September, 24, 0, 0, 0, 0, time.UTC)
	}
	exchange.lastFilledTime = lastFilledTime.Add(-6 * time.Hour)

	now := time.Now()
	filled, err := exchange.ListOrders(ctx, exchange.lastFilledTime, "FILLED")
	if err != nil {
		return nil, fmt.Errorf("could not fetch old filled orders: %w", err)
	}
	for _, v := range filled {
		if len(v.ClientOrderID) > 0 {
			exchange.clientOrderIDMap.Store(v.ClientOrderID, v)
		}
	}
	log.Printf("fetched %d filled orders from %s", len(filled), exchange.lastFilledTime)

	cancelled, err := exchange.ListOrders(ctx, exchange.lastFilledTime, "CANCELLED")
	if err != nil {
		return nil, fmt.Errorf("could not fetch old canceled orders: %w", err)
	}
	for _, v := range cancelled {
		if len(v.ClientOrderID) > 0 {
			exchange.clientOrderIDMap.Store(v.ClientOrderID, v)
		}
	}
	log.Printf("fetched %d canceled orders from %s", len(cancelled), lastFilledTime)

	exchange.lastFilledTime = now
	client.Go(exchange.goListOpenOrders)
	client.Go(exchange.goListFilledOrders)
	client.Go(exchange.goListCancelledOrders)
	return exchange, nil
}

func (ex *Exchange) Close() error {
	ex.client.Close()
	return nil
}

func (ex *Exchange) goListOpenOrders(ctx context.Context) {
	timeout := 5 * time.Second
	last := ex.lastFilledTime
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		from := ex.client.Now().Time.Add(-10 * time.Minute)
		if last.Before(from) {
			from = last
		}
		if _, err := ex.ListOrders(ctx, from, "OPEN"); err != nil {
			log.Printf("could not list OPEN orders from %s: %v", from, err)
			continue
		}
		last = from
	}
}

func (ex *Exchange) goListCancelledOrders(ctx context.Context) {
	timeout := 5 * time.Second
	last := ex.lastFilledTime
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		from := ex.client.Now().Time.Add(-10 * time.Minute)
		if last.Before(from) {
			from = last
		}
		if _, err := ex.ListOrders(ctx, from, "CANCELLED"); err != nil {
			log.Printf("could not list CANCELLED orders from %s: %v", from, err)
			continue
		}
		last = from
	}
}

func (ex *Exchange) goListFilledOrders(ctx context.Context) {
	timeout := 5 * time.Second
	last := ex.lastFilledTime
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		from := ex.client.Now().Time.Add(-10 * time.Minute)
		if last.Before(from) {
			from = last
		}
		if _, err := ex.ListOrders(ctx, from, "FILLED"); err != nil {
			log.Printf("could not list FILLED orders from %s: %v", from, err)
			continue
		}
		last = from
	}
}

// dispatchOrder relays the order fetched from coinbase for any reason to the
// appropriate product for side-channel handling.
func (ex *Exchange) dispatchOrder(productID string, order *exchange.Order) {
	ready := slices.Contains(readyStatuses, order.Status)
	done := slices.Contains(doneStatuses, order.Status)

	if ready || done {
		if ch, ok := ex.pendingMap.LoadAndDelete(order.ClientOrderID); ok {
			close(ch)
		}
	}

	ex.clientOrderIDMap.LoadOrStore(order.ClientOrderID, order)

	// Relay the order to the appropriate product.
	if p, ok := ex.productMap.Load(productID); ok {
		p.handleOrder(order)
	}
}

// dispatchMessage relays the websocket message to appropriate product.
func (ex *Exchange) dispatchMessage(msg *internal.Message) {
	if msg.Channel == "user" {
		for _, event := range msg.Events {
			if event.Type == "snapshot" || event.Type == "update" {
				for _, orderEvent := range event.Orders {
					v := exchangeOrderFromEvent(orderEvent)
					ex.dispatchOrder(orderEvent.ProductID, v)
				}
			}
		}
	}

	if msg.Channel == "ticker" {
		timestamp, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
		if err != nil {
			log.Printf("error: could not parse websocket msg timestamp %q (ignored): %v", msg.Timestamp, err)
			return
		}
		for _, event := range msg.Events {
			for _, ticker := range event.Tickers {
				if p, ok := ex.productMap.Load(ticker.ProductID); ok {
					p.handleTickerEvent(timestamp, ticker)
				}
			}
		}
	}
}

func (ex *Exchange) createReadyOrder(ctx context.Context, req *internal.CreateOrderRequest) (*internal.CreateOrderResponse, error) {
	statusReadyCh := make(chan struct{})
	if v, loaded := ex.pendingMap.LoadOrStore(req.ClientOrderID, statusReadyCh); loaded {
		log.Printf("unexpected: client id %s already exists in the pending map (ignored)", req.ClientOrderID)
		statusReadyCh = v
	}

	resp, err := ex.client.CreateOrder(ctx, req)

	if err == nil && resp.Success {
		// Wait for the order-id to be *ready*. We cannot cancel an order-id unless
		// it reaches to the OPEN status. We just wait here so that Cancel is
		// guaranteed to work for the callers after this function returns.
		for stop := false; stop == false; {
			select {
			case <-ctx.Done():
				return resp, err
			case <-statusReadyCh:
				stop = true
			case <-time.After(time.Second):
				log.Printf("warning: client order id %s created with server order id %s  (%s) in %s is not ready in time (forcing a fetch)", req.ClientOrderID, resp.OrderID, req.Side, req.ProductID)
				ex.GetOrder(ctx, exchange.OrderID(resp.OrderID))
			}
		}
	}

	return resp, err
}

func (ex *Exchange) recreateOldOrder(clientOrderID string) (*exchange.Order, bool) {
	old, ok := ex.clientOrderIDMap.Load(clientOrderID)
	if !ok {
		return nil, false
	}
	log.Printf("recreate order request for already used client-id %s is short-circuited to return old server order id %s", clientOrderID, old.OrderID)
	return old, true
}

func (ex *Exchange) GetOrder(ctx context.Context, orderID exchange.OrderID) (*exchange.Order, error) {
	resp, err := ex.client.GetOrder(ctx, string(orderID))
	if err != nil {
		return nil, fmt.Errorf("could not get order %s: %w", orderID, err)
	}
	v := exchangeOrderFromOrder(resp.Order)
	ex.dispatchOrder(resp.Order.ProductID, v)
	return v, nil
}

func (ex *Exchange) SyncFilled(ctx context.Context, from time.Time) error {
	from = from.Truncate(time.Hour)
	if _, err := ex.listRawOrders(ctx, from, "FILLED"); err != nil {
		return fmt.Errorf("could not fetch orders: %w", err)
	}
	return context.Cause(ctx)
}

func (ex *Exchange) SyncCancelled(ctx context.Context, from time.Time) error {
	from = from.Truncate(time.Hour)
	if _, err := ex.listRawOrders(ctx, from, "CANCELLED"); err != nil {
		return fmt.Errorf("could not fetch orders: %w", err)
	}
	return context.Cause(ctx)
}

func (ex *Exchange) listRawOrders(ctx context.Context, from time.Time, status string) ([]*internal.Order, error) {
	var result []*internal.Order

	values := make(url.Values)
	values.Add("limit", "100")
	values.Add("start_date", from.UTC().Format(time.RFC3339))
	values.Add("order_status", status)
	for i := 0; i == 0 || values != nil; i++ {
		resp, cont, err := ex.client.ListOrders(ctx, values)
		if err != nil {
			return nil, err
		}
		values = cont

		for _, order := range resp.Orders {
			if order != nil {
				result = append(result, order)
				ex.rawOrderTopic.Send(order)
			}
		}
	}

	return result, nil
}

func (ex *Exchange) GetProduct(ctx context.Context, productID string) (*gobs.Product, error) {
	resp, err := ex.client.GetProduct(ctx, productID)
	if err != nil {
		return nil, fmt.Errorf("could not fetch product %q info: %w", productID, err)
	}

	product := &gobs.Product{
		ProductID: resp.ProductID,
		Status:    resp.Status,
		Price:     resp.Price.Decimal,

		BaseName:          resp.BaseName,
		BaseMinSize:       resp.BaseMinSize.Decimal,
		BaseMaxSize:       resp.BaseMaxSize.Decimal,
		BaseIncrement:     resp.BaseIncrement.Decimal,
		BaseDisplaySymbol: resp.BaseDisplaySymbol,
		BaseCurrencyID:    resp.BaseCurrencyID,

		QuoteName:          resp.QuoteName,
		QuoteMinSize:       resp.QuoteMinSize.Decimal,
		QuoteMaxSize:       resp.QuoteMaxSize.Decimal,
		QuoteIncrement:     resp.QuoteIncrement.Decimal,
		QuoteDisplaySymbol: resp.QuoteDisplaySymbol,
		QuoteCurrencyID:    resp.QuoteCurrencyID,
	}
	return product, nil
}

func (ex *Exchange) GetCandles(ctx context.Context, productID string, from time.Time) ([]*gobs.Candle, error) {
	// Coinbase is not returning the candle with start time exactly equal to the
	// req.StartTime, so we adjust startTime by a second.
	from = from.Add(-time.Second)
	end := from.Add(300 * time.Minute)

	values := make(url.Values)
	values.Set("start", fmt.Sprintf("%d", from.Unix()))
	values.Set("end", fmt.Sprintf("%d", end.Unix()))
	values.Set("granularity", "ONE_MINUTE")

	resp, err := ex.client.GetProductCandles(ctx, productID, values)
	if err != nil {
		return nil, err
	}

	var cs []*gobs.Candle
	for _, c := range resp.Candles {
		start := time.Unix(c.Start, 0).UTC()
		gc := &gobs.Candle{
			StartTime: gobs.RemoteTime{Time: start},
			Duration:  time.Minute,
			Low:       c.Low.Decimal,
			High:      c.High.Decimal,
			Open:      c.Open.Decimal,
			Close:     c.Close.Decimal,
			Volume:    c.Volume.Decimal,
		}
		cs = append(cs, gc)
	}
	return cs, nil
}

func (ex *Exchange) IsDone(status string) bool {
	return slices.Contains(doneStatuses, status)
}
