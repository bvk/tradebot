// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
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
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Exchange struct {
	opts Options

	client *internal.Client

	websocket *internal.Websocket

	// clientOrderIDMap holds client-order-id to exchange.Order mapping for all
	// known orders. TODO: We should cleanup the oldest orders.
	clientOrderIDMap syncmap.Map[uuid.UUID, *exchange.SimpleOrder]

	productMap syncmap.Map[string, *Product]

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
	pendingMap syncmap.Map[uuid.UUID, chan struct{}]
}

var _ exchange.Exchange = &Exchange{}

// New creates a client for coinbase exchange.
func New(ctx context.Context, db kv.Database, kid, pem string, opts *Options) (_ *Exchange, status error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()

	copts := &internal.Options{
		RestHostname:           opts.RestHostname,
		WebsocketHostname:      opts.WebsocketHostname,
		HttpClientTimeout:      opts.HttpClientTimeout,
		WebsocketRetryInterval: opts.WebsocketRetryInterval,
		MaxTimeAdjustment:      opts.MaxTimeAdjustment,
		MaxFetchTimeLatency:    opts.MaxFetchTimeLatency,
	}
	client, err := internal.New(ctx, kid, pem, copts)
	if err != nil {
		slog.Error("could not create coinbase client", "err", err)
		return nil, fmt.Errorf("could not create coinbase client: %w", err)
	}
	defer func() {
		if status != nil {
			client.Close()
		}
	}()

	ps, err := client.ListProducts(ctx, "SPOT")
	if err != nil {
		slog.Error("could not list spot products", "err", err)
		return nil, fmt.Errorf("could not list coinbase products: %w", err)
	}
	pids := make([]string, 0, len(ps.Products))
	for _, p := range ps.Products {
		if p.QuoteDisplaySymbol == "USD" || p.QuoteDisplaySymbol == "USDC" {
			pids = append(pids, p.ProductID)
		}
	}

	exchange := &Exchange{
		opts:      *opts,
		client:    client,
		datastore: NewDatastore(db),
	}

	// User channel is subscribed for all supported products in a separate
	// connection from product specific channels.
	if !opts.subcmdMode {
		exchange.websocket = client.GetMessages("heartbeats", pids, exchange.dispatchMessage)
		exchange.websocket.Subscribe("user", pids)
	}

	// Find out the last saved timestamp and fetch all FILLED and CANCELLED
	// orders from that timestamp (with some hours overlap).
	lastFilledTime, err := exchange.datastore.LastFilledTime(ctx)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not find out last filled time: %w", err)
		}
		lastFilledTime = time.Date(2023, time.September, 24, 0, 0, 0, 0, time.UTC)
		log.Printf("datastore has no orders data (using %s as the last filled time)", lastFilledTime)
	} else {
		log.Printf("datastore seems to have orders data up to %s", lastFilledTime)
	}
	exchange.lastFilledTime = lastFilledTime.Add(-6 * time.Hour)

	if !opts.subcmdMode {
		if err := exchange.sync(ctx); err != nil {
			slog.Error("could not sync with coinbase to fix any lost data", "err", err)
			return nil, fmt.Errorf("could not sync for lost data: %w", err)
		}

		client.Go(exchange.goFetchProducts)
		client.Go(exchange.goFetchCandles)

		client.Go(func(ctx context.Context) {
			exchange.goRunBackgroundTasks(ctx)
		})
	}
	return exchange, nil
}

func (ex *Exchange) Close() error {
	ex.client.Close()
	return nil
}

func (ex *Exchange) ExchangeName() string {
	return "coinbase"
}

func (ex *Exchange) CanDedupOnClientUUID() bool {
	return true
}

func (ex *Exchange) sync(ctx context.Context) error {
	filled, err := ex.ListOrders(ctx, ex.lastFilledTime, "FILLED")
	if err != nil {
		slog.Error("could not fetch filled orders", "lastFilledTime", ex.lastFilledTime, "err", err)
		return fmt.Errorf("could not fetch old filled orders: %w", err)
	}
	for _, v := range filled {
		ex.clientOrderIDMap.Store(v.ClientID(), v)
	}

	log.Printf("fetched %d filled orders from %s", len(filled), ex.lastFilledTime)

	// FIXME: Number of cancelled orders can be huge. We probably don't need to
	// fetch all cancelled orders.
	cancelled, err := ex.ListOrders(ctx, ex.lastFilledTime, "CANCELLED")
	if err != nil {
		slog.Error("could not fetch canceled orders", "fromTime", ex.lastFilledTime, "err", err)
		return fmt.Errorf("could not fetch old canceled orders: %w", err)
	}
	for _, v := range cancelled {
		ex.clientOrderIDMap.Store(v.ClientID(), v)
	}
	log.Printf("fetched %d canceled orders from %s", len(cancelled), ex.lastFilledTime)
	return nil
}

func (ex *Exchange) goFetchProducts(ctx context.Context) {
	timeout := ex.opts.FetchProductsInterval
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		resp, err := ex.client.ListProducts(ctx, "SPOT")
		if err != nil {
			slog.Warn("could not list spot products (will retry)", "err", err)
			continue
		}
		if err := ex.datastore.saveProducts(ctx, resp.Products); err != nil {
			slog.Warn("could not save products list (will retry)", "err", err)
			continue
		}
	}
}

func (ex *Exchange) goFetchCandles(ctx context.Context) {
	timeout := ex.opts.FetchCandlesInterval
	if timeout < 0 {
		return
	}

	var last time.Time
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		if last.IsZero() {
			v, err := ex.datastore.lastCandlesTime(ctx)
			if err != nil {
				slog.Warn("could not determine last fetched candles time (will retry)", "err", err)
				continue
			}
			last = v
			log.Printf("candles data seems to be available in the datastore upto time %s", last)
		}

		now := ex.client.Now().Time

		failed := false
		for _, pid := range ex.opts.WatchProductIDs {
			if err := ex.SyncCandles(ctx, pid, last, now); err != nil {
				slog.Warn("could not fetch candles (will retry)", "err", err)
				failed = true
				break
			}
		}

		if !failed {
			last = now
		}
	}
}

func (ex *Exchange) goRunBackgroundTasks(ctx context.Context) {
	last := ex.lastFilledTime
	timeout := ex.opts.PollOrdersRetryInterval
	accountHoldMap := make(map[string]decimal.Decimal)
	accountAvailableMap := make(map[string]decimal.Decimal)
	for ctxutil.Sleep(ctx, timeout); ctx.Err() == nil; ctxutil.Sleep(ctx, timeout) {
		now := ex.client.Now().Time
		fills, err := ex.listFillsFrom(ctx, last)
		if err != nil {
			slog.Warn("could not list fills", "fromTime", last, "err", err)
			continue
		}
		if len(fills) > 0 {
			log.Printf("fetched %d fills after %s", len(fills), last)
		}

		failed := false
		var orders []*internal.Order
		for _, fill := range fills {
			resp, err := ex.client.GetOrder(ctx, fill.OrderID)
			if err != nil {
				slog.Warn("could not get order", "order", fill.OrderID, "err", err)
				failed = true
				continue
			}
			// Process the order for notifications and datastore.
			v, err := exchangeOrderFromOrder(resp.Order)
			if err != nil {
				slog.Error("could not convert to simple order (ignored)", "err", err)
				continue
			}
			ex.dispatchOrder(resp.Order.ProductID, v)
			orders = append(orders, resp.Order)
		}

		if len(orders) > 0 {
			if err := ex.datastore.maybeSaveOrders(ctx, orders); err != nil {
				slog.Warn("could not save filled orders (will retry)", "err", err)
				continue
			}
		}

		if !failed {
			last = now
		}

		// Update account balances.
		accounts, err := ex.listRawAccounts(ctx)
		if err != nil {
			slog.Warn("could not fetch account balances (will retry)", "err", err)
			continue
		}
		if err := ex.datastore.saveAccounts(ctx, accounts); err != nil {
			slog.Warn("could not save account balances (will retry)", "err", err)
		}
		// Write account balances changes to the logs.
		for _, a := range accounts {
			lastHold, ok1 := accountHoldMap[a.Hold.Currency]
			lastAvail, ok2 := accountAvailableMap[a.AvailableBalance.Currency]
			newHold := a.Hold.Value.Decimal
			newAvail := a.AvailableBalance.Value.Decimal
			if ok1 && ok2 && lastHold.Equal(newHold) && lastAvail.Equal(newAvail) {
				continue
			}
			accountHoldMap[a.Hold.Currency] = newHold
			accountAvailableMap[a.AvailableBalance.Currency] = newAvail
			slog.Info("coinbase account balance", "name", a.Name, "currency", a.Currency, "available", newAvail, "hold", newHold)
		}
	}
}

// dispatchOrder relays the order fetched from coinbase for any reason to the
// appropriate product for side-channel handling.
func (ex *Exchange) dispatchOrder(productID string, order *exchange.SimpleOrder) {
	if len(order.ServerOrderID) == 0 {
		slog.Error("unexpected relay request with empty server order id is ignored")
		return
	}

	ready := slices.Contains(readyStatuses, order.Status)
	done := slices.Contains(doneStatuses, order.Status)

	if ready || done {
		if ch, ok := ex.pendingMap.LoadAndDelete(order.ClientID()); ok {
			close(ch)
		}
	}

	ex.clientOrderIDMap.LoadOrStore(order.ClientID(), order)

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
					v, err := exchangeOrderFromEvent(orderEvent)
					if err != nil {
						slog.Error("could not convert order event to simple order (ignored)", "err", err)
						continue
					}
					ex.dispatchOrder(orderEvent.ProductID, v)
				}
			}
		}
	}

	if msg.Channel == "ticker" {
		timestamp, err := time.Parse(time.RFC3339Nano, msg.Timestamp)
		if err != nil {
			slog.Error("could not parse websocket msg (ignored)", "timestamp", msg.Timestamp, "err", err)
			return
		}
		for _, event := range msg.Events {
			for _, ticker := range event.Tickers {
				ticker.Timestamp = gobs.RemoteTime{Time: timestamp}
				if p, ok := ex.productMap.Load(ticker.ProductID); ok {
					p.handleTickerEvent(timestamp, ticker)
				}
				if !strings.HasSuffix(ticker.ProductID, "-USD") {
					continue
				}
				usdcProduct := strings.TrimSuffix(ticker.ProductID, "-USD") + "-USDC"
				if p, ok := ex.productMap.Load(usdcProduct); ok {
					p.handleTickerEvent(timestamp, ticker)
				}
			}
		}
	}
}

func (ex *Exchange) createReadyOrder(ctx context.Context, req *internal.CreateOrderRequest) (*internal.CreateOrderResponse, error) {
	cuuid, err := uuid.Parse(req.ClientOrderID)
	if err != nil {
		return nil, fmt.Errorf("could not parse client order id as uuid: %w", err)
	}
	statusReadyCh := make(chan struct{})
	if v, loaded := ex.pendingMap.LoadOrStore(cuuid, statusReadyCh); loaded {
		slog.Error("unexpected: client id already exists in the pending map (previous request may've failed; ignored)", "client-order-id", req.ClientOrderID)
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
				slog.Warn(fmt.Sprintf("client order id %s created with server order id %s  (%s) in %s is not ready in time (forcing a fetch)", req.ClientOrderID, resp.SuccessResponse.OrderID, req.Side, req.ProductID))
				ex.GetOrder(ctx, "" /* productID */, resp.SuccessResponse.OrderID)
			}
		}
	}

	return resp, err
}

func (ex *Exchange) recreateOldOrder(clientOrderID uuid.UUID) (*exchange.SimpleOrder, bool) {
	old, ok := ex.clientOrderIDMap.Load(clientOrderID)
	if !ok {
		return nil, false
	}
	log.Printf("recreate order request for already used client-id %s is short-circuited to return old server order id %s", clientOrderID, old.ServerOrderID)
	return old, true
}

func (ex *Exchange) GetOrder(ctx context.Context, _ string, orderID string) (exchange.OrderDetail, error) {
	if v, err := ex.datastore.GetOrder(ctx, string(orderID)); err == nil {
		return exchangeOrderFromOrder(v)
	}
	resp, err := ex.client.GetOrder(ctx, string(orderID))
	if err != nil {
		return nil, fmt.Errorf("could not get order %s: %w", orderID, err)
	}
	v, err := exchangeOrderFromOrder(resp.Order)
	if err != nil {
		return nil, err
	}
	ex.dispatchOrder(resp.Order.ProductID, v)
	return v, nil
}

func (ex *Exchange) SyncFilled(ctx context.Context, from time.Time) error {
	from = from.Truncate(time.Hour)
	orders, err := ex.listRawOrders(ctx, from, "FILLED")
	if err != nil {
		return fmt.Errorf("could not fetch orders: %w", err)
	}
	if len(orders) > 0 {
		if err := ex.datastore.maybeSaveOrders(ctx, orders); err != nil {
			slog.Error("could not save filled orders to the data store", "err", err)
			return fmt.Errorf("could not save orders: %w", err)
		}
	}
	return nil
}

func (ex *Exchange) SyncCancelled(ctx context.Context, from time.Time) error {
	from = from.Truncate(time.Hour)
	orders, err := ex.listRawOrders(ctx, from, "CANCELLED")
	if err != nil {
		return fmt.Errorf("could not fetch orders: %w", err)
	}
	if len(orders) > 0 {
		if err := ex.datastore.maybeSaveOrders(ctx, orders); err != nil {
			slog.Error("could not save canceled orders to the data store", "err", err)
			return fmt.Errorf("could not save orders: %w", err)
		}
	}
	return nil
}

func (ex *Exchange) listFillsFrom(ctx context.Context, from time.Time) ([]*internal.Fill, error) {
	var result []*internal.Fill

	values := make(url.Values)
	values.Add("limit", "100")
	values.Add("start_sequence_timestamp", from.UTC().Format(time.RFC3339))
	for i := 0; i == 0 || values != nil; i++ {
		resp, cont, err := ex.client.ListFills(ctx, values)
		if err != nil {
			slog.Error("could not list fills", "from", from, "err", err)
			return nil, err
		}
		values = cont

		for _, fill := range resp.Fills {
			if fill != nil {
				result = append(result, fill)
			}
		}
	}
	return result, nil
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
			slog.Error("could not list orders", "from", from, "status", status, "err", err)
			return nil, err
		}
		values = cont

		for _, order := range resp.Orders {
			if order != nil {
				result = append(result, order)
			}
		}
	}

	return result, nil
}

func (ex *Exchange) ListOrders(ctx context.Context, from time.Time, status string) ([]*exchange.SimpleOrder, error) {
	var orders []*exchange.SimpleOrder
	rorders, err := ex.listRawOrders(ctx, from, status)
	if err != nil {
		return nil, fmt.Errorf("could not list raw orders: %w", err)
	}
	for _, order := range rorders {
		v, err := exchangeOrderFromOrder(order)
		if err != nil {
			return nil, fmt.Errorf("could not convert to simple order: %w", err)
		}
		ex.dispatchOrder(order.ProductID, v)
		orders = append(orders, v)
	}
	return orders, nil
}

func (ex *Exchange) listRawAccounts(ctx context.Context) ([]*internal.Account, error) {
	var accounts []*internal.Account

	values := make(url.Values)
	for i := 0; i == 0 || values != nil; i++ {
		resp, cont, err := ex.client.ListAccounts(ctx, values)
		if err != nil {
			return nil, err
		}
		values = cont
		accounts = append(accounts, resp.Accounts...)
	}

	return accounts, nil
}

func (ex *Exchange) GetSpotProduct(ctx context.Context, base, quote string) (*gobs.Product, error) {
	productID := fmt.Sprintf("%s-%s", strings.ToUpper(base), strings.ToUpper(quote))

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

// SyncCandles fetches `ONE_MINUTE` candles from coinbase between the `begin`
// and `end` timestamps for a single product and saves them to the datastore.
func (ex *Exchange) SyncCandles(ctx context.Context, productID string, begin, end time.Time) error {
	cmp := func(a, b *internal.Candle) int {
		if a.Start == b.Start {
			return 0
		}
		if a.Start < b.Start {
			return -1
		}
		return 1
	}

	last := begin.UTC()
	for last.Before(end) {
		candles, err := ex.getRawCandles(ctx, productID, last)
		if err != nil {
			return fmt.Errorf("could not fetch candles from coinbase: %w", err)
		}

		ncandles := len(candles)
		if ncandles == 0 {
			break
		}

		slices.SortFunc(candles, cmp)
		last = time.Unix(candles[ncandles-1].Start, 0).Add(time.Second).UTC()
		if err := ex.datastore.saveCandles(ctx, productID, candles); err != nil {
			return fmt.Errorf("could not save candles to datastore: %w", err)
		}
	}
	return nil
}

// getRawCandles fetches `ONE_MINUTE` candles from coinbase starting at `from`
// timestamp. Returns maximum of 300 candles.
func (ex *Exchange) getRawCandles(ctx context.Context, productID string, from time.Time) ([]*internal.Candle, error) {
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

	return resp.Candles, nil
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
