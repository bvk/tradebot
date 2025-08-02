// Copyright (c) 2025 BVK Chaitanya

package coinex

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/coinex/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/syncmap"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

type clientIDStatus struct {
	mu sync.Mutex

	err error

	order *internal.Order

	status string

	timestampMilli int64
}

func newClientIDStatus() *clientIDStatus {
	return &clientIDStatus{timestampMilli: time.Now().UnixMilli()}
}

func (v *clientIDStatus) isValidLocked() bool {
	return v.err == nil && v.order != nil
}

func (v *clientIDStatus) isDoneLocked() bool {
	return strings.EqualFold(v.status, "filled") || strings.EqualFold(v.status, "canceled")
}

type Product struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	client *Client

	market string

	mstatus *internal.MarketStatus
	minfo   *internal.MarketInfo

	clientIDStatusMap syncmap.Map[uuid.UUID, *clientIDStatus]

	failedCreatesCh chan uuid.UUID
}

var _ exchange.Product = &Product{}

func NewProduct(ctx context.Context, client *Client, market string) (*Product, error) {
	mstatus, err := client.GetMarket(ctx, market)
	if err != nil {
		return nil, err
	}
	// TODO: Check that market is online.
	minfo, err := client.GetMarketInfo(ctx, market)
	if err != nil {
		return nil, err
	}
	if err := client.WatchMarket(ctx, market); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	p := &Product{
		lifeCtx:         lifeCtx,
		lifeCancel:      lifeCancel,
		market:          market,
		client:          client,
		minfo:           minfo,
		mstatus:         mstatus,
		failedCreatesCh: make(chan uuid.UUID, 100),
	}

	// Fetch old filled and unfilled orders upto a limit and prepare the initial
	// clientID status map.
	target := time.Now().Add(-24 * time.Hour)
	for order := range client.ListFilledOrders(ctx, market, "" /* side */, &err) {
		if order.CreatedAt().Time.Before(target) {
			break
		}
		cid := order.ClientID()
		cstatus := &clientIDStatus{
			order:          order,
			timestampMilli: order.CreatedAtMilli,
			status:         "filled",
		}
		p.clientIDStatusMap.Store(cid, cstatus)
	}
	if err != nil {
		return nil, err
	}
	for order := range client.ListUnfilledOrders(ctx, market, "" /* side */, &err) {
		if order.CreatedAt().Time.Before(target) {
			break
		}
		cid := order.ClientID()
		cstatus := &clientIDStatus{
			order:          order,
			timestampMilli: order.CreatedAtMilli,
		}
		p.clientIDStatusMap.Store(cid, cstatus)
	}
	if err != nil {
		return nil, err
	}

	// Start goroutines to watch, refresh and cleanup clientIDStatus map.
	p.wg.Add(1)
	go p.goRefreshOrders(p.lifeCtx)

	p.wg.Add(1)
	go p.goWatchOrderUpdates(p.lifeCtx)

	p.wg.Add(1)
	go p.goCancelFailedCreates(p.lifeCtx)

	// TODO: Also, cleanup clientIDStatusMap.
	return p, nil
}

func (p *Product) Close() error {
	p.lifeCancel(os.ErrClosed)
	p.wg.Wait()
	return nil
}

func (p *Product) ProductID() string {
	return p.market
}

func (p *Product) ExchangeName() string {
	return "coinex"
}

func (p *Product) BaseMinSize() decimal.Decimal {
	return p.mstatus.MinAmount
}

func (p *Product) GetOrderUpdates() (*topic.Receiver[exchange.OrderUpdate], error) {
	fn := func(x *internal.Order) exchange.OrderUpdate { return x }
	return topic.SubscribeFunc(p.client.getMarketOrdersTopic(p.market), fn, 0, true)
}

func (p *Product) GetPriceUpdates() (*topic.Receiver[exchange.PriceUpdate], error) {
	fn := func(x *internal.BBOUpdate) exchange.PriceUpdate { return x }
	return topic.SubscribeFunc(p.client.getMarketPricesTopic(p.market), fn, 1, true)
}

func (p *Product) LimitBuy(ctx context.Context, clientOrderID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	cstatus, loaded := p.clientIDStatusMap.LoadOrStore(clientOrderID, newClientIDStatus())
	cstatus.mu.Lock()
	defer cstatus.mu.Unlock()

	// Deduplicate client-order-ids. CoinEx server doesn't dedup for us.
	if loaded {
		if cstatus.err != nil {
			return nil, cstatus.err
		}
		return cstatus.order, nil
	}
	defer func() {
		cstatus.err = status
	}()

	request := &internal.CreateOrderRequest{
		ClientOrderID: hex.EncodeToString(clientOrderID[:]),
		Market:        p.market,
		MarketType:    "SPOT",
		Side:          "buy",
		OrderType:     "limit",
		Amount:        size,
		Price:         price,
	}
	order, err := p.client.CreateOrder(ctx, request)
	if err != nil {
		p.failedCreatesCh <- clientOrderID
		return nil, err
	}

	cstatus.order = order
	return order, nil
}

func (p *Product) LimitSell(ctx context.Context, clientOrderID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	cstatus, loaded := p.clientIDStatusMap.LoadOrStore(clientOrderID, newClientIDStatus())
	cstatus.mu.Lock()
	defer cstatus.mu.Unlock()

	// Deduplicate client-order-ids. CoinEx server doesn't dedup for us.
	if loaded {
		if cstatus.err != nil {
			return nil, cstatus.err
		}
		return cstatus.order, nil
	}
	defer func() {
		cstatus.err = status
	}()

	request := &internal.CreateOrderRequest{
		ClientOrderID: hex.EncodeToString(clientOrderID[:]),
		Market:        p.market,
		MarketType:    "SPOT",
		Side:          "sell",
		OrderType:     "limit",
		Amount:        size,
		Price:         price,
	}
	order, err := p.client.CreateOrder(ctx, request)
	if err != nil {
		p.failedCreatesCh <- clientOrderID
		return nil, err
	}

	cstatus.order = order
	return order, nil
}

func (p *Product) Get(ctx context.Context, id string) (exchange.OrderDetail, error) {
	v, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return nil, err
	}
	order, err := p.client.GetOrder(ctx, p.market, v)
	if err != nil {
		return nil, err
	}
	return order, nil
}

func (p *Product) Cancel(ctx context.Context, id string) error {
	v, err := strconv.ParseInt(string(id), 10, 64)
	if err != nil {
		return err
	}
	if _, err := p.client.CancelOrder(ctx, p.market, v); err != nil {
		return err
	}
	return nil
}

func (p *Product) goRefreshOrders(ctx context.Context) {
	defer p.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("CAUGHT PANIC", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return

		case <-time.After(p.client.opts.RefreshOrdersInterval):
			if err := p.refreshOrders(ctx); err != nil {
				slog.Warn("could not refresh orders (will retry)", "err", err)
			}
		}
	}
}

func (p *Product) refreshOrders(ctx context.Context) error {
	ids := make([]int64, 0, p.client.opts.BatchQueryOrdersSize)
	id2cstatusMap := make(map[int64]*clientIDStatus)

	refresh := func() error {
		slog.Debug("refreshing status for orders", "ids", ids)
		resps, err := p.client.BatchQueryOrders(ctx, p.market, ids)
		if err != nil {
			return err
		}

		for i, resp := range resps {
			cstatus := id2cstatusMap[ids[i]]
			cstatus.mu.Lock()
			if resp.Code == 0 {
				cstatus.status = resp.Data.Status
			}
			if resp.Code == internal.OrderNotFound {
				cstatus.status = "canceled"
				slog.Debug("fixed order id status as *canceled* because order is not found", "id", ids[i], "code", resp.Code)
			}
			cstatus.mu.Unlock()
		}

		// Clear the batch.
		ids = ids[:0]
		return nil
	}

	for _, cstatus := range p.clientIDStatusMap.Range {
		cstatus.mu.Lock()
		if cstatus.isValidLocked() && !cstatus.isDoneLocked() {
			ids = append(ids, cstatus.order.OrderID)
			id2cstatusMap[cstatus.order.OrderID] = cstatus
		}
		cstatus.mu.Unlock()

		if len(ids) < p.client.opts.BatchQueryOrdersSize {
			continue
		}
		if err := refresh(); err != nil {
			return err
		}
	}

	if len(ids) > 0 {
		if err := refresh(); err != nil {
			return err
		}
	}
	return nil
}

func (p *Product) goWatchOrderUpdates(ctx context.Context) {
	defer p.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("CAUGHT PANIC", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	sub, err := topic.Subscribe(p.client.getMarketOrdersTopic(p.market), 0, true)
	if err != nil {
		slog.Error("could not subscribe to order updates topic", "err", err)
		return
	}
	defer sub.Close()

	stopf := context.AfterFunc(ctx, sub.Close)
	defer stopf()

	for ctx.Err() == nil {
		order, err := sub.Receive()
		if err != nil {
			return
		}
		cid := order.ClientID()
		cstatus, ok := p.clientIDStatusMap.Load(cid)
		if !ok {
			continue
		}
		cstatus.mu.Lock()
		cstatus.status = order.Status
		cstatus.mu.Unlock()
	}
}

// goCancelFailedCreates is a background goroutine that attempts to cancel
// buy/sell order that have unknown server order id -- which means they may
// have been successful or unsuccessful -- which could happen when response has
// timeout.
func (p *Product) goCancelFailedCreates(ctx context.Context) {
	defer p.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("CAUGHT PANIC", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	// retryMap holds a mapping from a failed client order id to a time point
	// after which, we must retry the cancel-by-client-id.
	retryMap := make(map[uuid.UUID]time.Time)
	retryBackoff := 0

	for {
		var retry uuid.UUID
		var retryCh chan uuid.UUID
		if len(retryMap) > 0 {
			now := time.Now()
			for cid, at := range retryMap {
				if at.After(now) {
					retry, retryCh = cid, p.failedCreatesCh
					break
				}
			}
		}

		select {
		case <-ctx.Done():
			return

		case retryCh <- retry:
			delete(retryMap, retry)

		case cid := <-p.failedCreatesCh:
			order, err := p.client.CancelOrderByClientID(ctx, p.market, hex.EncodeToString(cid[:]))
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					slog.Error("could not cancel order by client id (will retry)", "clientID", cid, "err", err, "retryAfter", time.Second<<retryBackoff)
					retryMap[cid] = time.Now().Add(time.Second << retryBackoff)
					retryBackoff = min(retryBackoff+1, 6)
					continue
				}
				retryBackoff = 0
				if cstatus, ok := p.clientIDStatusMap.Load(cid); ok {
					cstatus.mu.Lock()
					cstatus.status = "canceled"
					cstatus.mu.Unlock()
				}
				continue
			}

			retryBackoff = 0
			if cstatus, ok := p.clientIDStatusMap.Load(cid); ok {
				cstatus.mu.Lock()
				cstatus.status = "canceled"
				cstatus.order = order
				cstatus.mu.Unlock()
			}
			if !order.FilledAmount.IsZero() {
				slog.Warn("created order that had failed due to network issues has non-zero filled amount",
					"orderID", order.ServerID(), "clientID", order.ClientID(), "side", order.OrderSide(), "filledSize", order.ExecutedSize())
			}
		}
	}
}
