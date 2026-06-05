// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

// orderPlacementInfo holds the parameters of a PlaceOrder request. Used by
// goCancelFailedCreates to match the order in the open-orders list when the
// server order ID is unknown (E*TRADE omits clientOrderId from list responses).
// Fields are exported so gob can serialize them when embedded in orderEntry.
type orderPlacementInfo struct {
	RequestTimeMilli int64
	Side             string
	Price            decimal.Decimal
	Qty              decimal.Decimal
	PriceType        string
	OrderTerm        string
}

// clientIDStatus tracks the state of a single order keyed by the client order
// UUID. It is protected by its own mutex because LimitBuy/LimitSell,
// goWatchOrderUpdates, and goCancelFailedCreates all access it concurrently.
type clientIDStatus struct {
	mu sync.Mutex

	// err is set when order placement failed permanently. Subsequent calls with
	// the same clientOrderUUID return this error immediately.
	err error

	// order holds the most recent E*TRADE order state. Nil until placement
	// succeeds or a matching update arrives.
	order *internal.Order

	// placement holds the original request parameters, set before PlaceOrder is
	// called and used for recovery matching if the call times out or crashes.
	placement orderPlacementInfo
}

// orderEntry is the value persisted in the database for each order we place,
// keyed by the client order UUID. ServerOrderID is 0 until PlaceOrder returns
// successfully. Placement is written before PlaceOrder is called so that if
// the process crashes before the server order ID is known, startup
// reconciliation can match the order in the open-orders list by parameters.
type orderEntry struct {
	ServerOrderID int64
	Placement     orderPlacementInfo
}

// Product implements exchange.Product for a single E*TRADE equity symbol.
type Product struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	db     kv.Database
	client *Client
	symbol string

	// etradeOrderID generates unique numeric values to send as clientOrderId in
	// PlaceOrder requests. E*TRADE only accepts digits in this field so UUID
	// encoding is not possible. The value is not persisted or used for lookup.
	etradeOrderID atomic.Int64

	// clientIDStatusMap is keyed by the client order UUID and tracks order state
	// for deduplication and status queries within a session.
	clientIDStatusMap syncmap.Map[uuid.UUID, *clientIDStatus]

	// serverIDMap maps E*TRADE server order ID → client order UUID. Used by
	// Get() to restore ClientUUID without relying on the clientOrderId field,
	// which E*TRADE omits from single-order GET responses.
	serverIDMap syncmap.Map[int64, uuid.UUID]

	// failedCreatesCh receives UUIDs of orders whose PlaceOrder call returned
	// an error. goCancelFailedCreates scans open orders to find and cancel any
	// that were created despite the error.
	failedCreatesCh chan uuid.UUID
}

var _ exchange.Product = &Product{}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// matchOpenOrder finds the first order in candidates that matches the given
// symbol and placement parameters, within a 60-second window of the request time.
// Used to recover orders when the server ID is unknown (E*TRADE omits
// clientOrderId from list responses so we cannot match by that field).
func matchOpenOrder(candidates []*internal.Order, symbol string, p orderPlacementInfo) *internal.Order {
	const matchWindowMilli = 60_000
	for _, o := range candidates {
		if o.Symbol != symbol {
			continue
		}
		if !strings.EqualFold(o.Side, p.Side) {
			continue
		}
		if !strings.EqualFold(o.PriceType, p.PriceType) {
			continue
		}
		if !strings.EqualFold(o.OrderTerm, p.OrderTerm) {
			continue
		}
		if !o.LimitPrice.Equal(p.Price) {
			continue
		}
		if !o.OrderedQty.Equal(p.Qty) {
			continue
		}
		if absInt64(o.PlacedTimeMilli-p.RequestTimeMilli) > matchWindowMilli {
			continue
		}
		return o
	}
	return nil
}

// clientOrderKeyPrefix returns the DB key prefix for all client orders of this symbol.
func (p *Product) clientOrderKeyPrefix() string {
	return fmt.Sprintf("/etrade/symbol/%s/clientorder", p.symbol)
}

// clientOrderKey returns the DB key for a client order UUID.
func (p *Product) clientOrderKey(clientOrderUUID uuid.UUID) string {
	return p.clientOrderKeyPrefix() + "/" + clientOrderUUID.String()
}

// clientOrderUUIDFromKey parses the client order UUID from a DB key produced by clientOrderKey.
func (p *Product) clientOrderUUIDFromKey(key string) (uuid.UUID, error) {
	parts := strings.Split(key, "/")
	uid, err := uuid.Parse(parts[len(parts)-1])
	if err != nil {
		return uuid.Nil, fmt.Errorf("etrade: invalid order key %q: %w", key, err)
	}
	return uid, nil
}

// dbSetEntry persists an orderEntry to the database keyed by client order UUID.
func (p *Product) dbSetEntry(ctx context.Context, clientOrderUUID uuid.UUID, entry orderEntry) {
	key := p.clientOrderKey(clientOrderUUID)
	if err := kvutil.SetDB(ctx, p.db, key, &entry); err != nil {
		slog.Warn("etrade: could not persist order entry", "symbol", p.symbol, "clientOrderUUID", clientOrderUUID, "err", err)
	}
}

// dbDeleteEntry removes an orderEntry from the database.
func (p *Product) dbDeleteEntry(ctx context.Context, clientOrderUUID uuid.UUID) {
	key := p.clientOrderKey(clientOrderUUID)
	err := kv.WithReadWriter(ctx, p.db, func(ctx context.Context, rw kv.ReadWriter) error {
		return rw.Delete(ctx, key)
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("etrade: could not delete order entry", "symbol", p.symbol, "clientOrderUUID", clientOrderUUID, "err", err)
	}
}

// NewProduct creates a Product for the given equity symbol. It registers the
// symbol for price polling, loads any persisted counterID entries, starts
// background goroutines, and then reconciles open orders from the previous
// session as the last step so that goWatchOrderUpdates is already subscribed
// when goRefreshOrders publishes any recovered order updates.
func NewProduct(ctx context.Context, db kv.Database, client *Client, symbol string) (*Product, error) {
	// Register the symbol with the client so goPollPrices starts fetching it.
	client.getSymbolPriceTopic(symbol)

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	p := &Product{
		lifeCtx:         lifeCtx,
		lifeCancel:      lifeCancel,
		db:              db,
		client:          client,
		symbol:          symbol,
		failedCreatesCh: make(chan uuid.UUID, 100),
	}
	p.etradeOrderID.Store(time.Now().UnixNano())

	// Load persisted order entries from the previous session and collect them
	// for reconciliation after goroutines start.
	type pendingEntry struct {
		uid   uuid.UUID
		entry orderEntry
	}
	var pending []pendingEntry
	begin, end := kvutil.PathRange(p.clientOrderKeyPrefix())
	loadErr := kvutil.AscendDB(ctx, db, begin, end, func(ctx context.Context, _ kv.Reader, key string, e *orderEntry) error {
		uid, err := p.clientOrderUUIDFromKey(key)
		if err != nil {
			return err
		}
		if e.ServerOrderID != 0 {
			p.serverIDMap.Store(e.ServerOrderID, uid)
		}
		pending = append(pending, pendingEntry{uid, *e})
		return nil
	})
	if loadErr != nil {
		lifeCancel(loadErr)
		return nil, fmt.Errorf("etrade: could not load persisted counterID entries for %s: %w", symbol, loadErr)
	}

	// Start goroutines before reconciling so that goWatchOrderUpdates is
	// subscribed to the symbol orders topic before goRefreshOrders (via
	// TrackOrder) begins publishing recovered order updates.
	p.wg.Add(2)
	go p.goWatchOrderUpdates(p.lifeCtx)
	go p.goCancelFailedCreates(p.lifeCtx)

	// Reconcile each persisted entry as the last step. For entries with a known
	// server order ID, fetch current state from E*TRADE. For entries with
	// ServerOrderID==0 (PlaceOrder outcome was unknown), fetch the open orders
	// list once (lazily) to determine if the order was created.
	//
	// openOrders is fetched at most once and cached across iterations.
	var openOrders []*internal.Order
	var openOrdersFetched bool
	for _, pe := range pending {
		uid := pe.uid
		entry := pe.entry

		if entry.ServerOrderID == 0 {
			// PlaceOrder may not have completed before the previous session ended.
			// Fetch open orders exactly once; if unavailable defer to goCancelFailedCreates.
			if !openOrdersFetched {
				openOrdersFetched = true
				var listErr error
				openOrders, listErr = p.client.ListOpenOrders(ctx)
				if listErr != nil {
					slog.Warn("etrade: startup: could not list open orders; deferring ServerOrderID=0 reconciliation",
						"symbol", symbol, "err", listErr)
				} else {
					count := 0
					for _, o := range openOrders {
						if o.Symbol == symbol {
							count++
						}
					}
					slog.Info("etrade: open orders found for symbol at startup", "symbol", symbol, "count", count)
				}
			}
			if openOrders == nil {
				// ListOpenOrders unavailable; defer to goCancelFailedCreates.
				p.clientIDStatusMap.Store(uid, &clientIDStatus{placement: entry.Placement})
				p.failedCreatesCh <- uid
				continue
			}
			found := matchOpenOrder(openOrders, symbol, entry.Placement)
			if found == nil {
				// Not in open orders: either never created, or filled/cancelled
				// before we could check. Remove the entry.
				slog.Warn("etrade: startup: order with unknown server ID not found in open orders; removing",
					"symbol", symbol, "uid", uid)
				p.dbDeleteEntry(ctx, uid)
				continue
			}
			// Found in open orders. Persist the server order ID now that we have it.
			entry.ServerOrderID = found.OrderID
			p.dbSetEntry(ctx, uid, entry)
			p.serverIDMap.Store(found.OrderID, uid)
			found.ClientUUID = uid
			p.clientIDStatusMap.Store(uid, &clientIDStatus{order: found})
			p.client.TrackOrder(found.OrderID)
			continue
		}

		// Fetch current order state using the persisted server order ID.
		order, err := p.client.GetOrder(ctx, entry.ServerOrderID)
		if err != nil {
			slog.Warn("etrade: startup: could not fetch order state; will rely on polling to recover",
				"symbol", symbol, "uid", uid, "serverOrderID", entry.ServerOrderID, "err", err)
			// Leave the entry in the DB. Re-register for tracking so polling
			// picks it up and the topic update eventually reaches goWatchOrderUpdates.
			p.client.TrackOrder(entry.ServerOrderID)
			continue
		}
		p.serverIDMap.Store(entry.ServerOrderID, uid)
		order.ClientUUID = uid
		p.clientIDStatusMap.Store(uid, &clientIDStatus{order: order})
		if order.IsDone() {
			p.dbDeleteEntry(ctx, uid)
		} else {
			p.client.TrackOrder(entry.ServerOrderID)
		}
	}

	return p, nil
}

func (p *Product) Close() error {
	p.lifeCancel(os.ErrClosed)
	p.wg.Wait()
	return nil
}

func (p *Product) ProductID() string {
	return p.symbol
}

func (p *Product) ExchangeName() string {
	return "etrade"
}

// BaseMinSize returns the minimum order size for US equity symbols: 1 share.
func (p *Product) BaseMinSize() decimal.Decimal {
	return decimal.NewFromInt(1)
}

func (p *Product) GetOrderUpdates() (*topic.Receiver[exchange.OrderUpdate], error) {
	fn := func(o *internal.Order) exchange.OrderUpdate {
		if o.ClientUUID != uuid.Nil {
			return o
		}
		uid, ok := p.serverIDMap.Load(o.OrderID)
		if !ok {
			return o
		}
		clone := *o
		clone.ClientUUID = uid
		return &clone
	}
	return topic.SubscribeFunc(p.client.getSymbolOrdersTopic(p.symbol), fn, 0, true)
}

func (p *Product) GetPriceUpdates() (*topic.Receiver[exchange.PriceUpdate], error) {
	fn := func(q *internal.Quote) exchange.PriceUpdate { return q }
	return topic.SubscribeFunc(p.client.getSymbolPriceTopic(p.symbol), fn, 1, true)
}

func (p *Product) LimitBuy(ctx context.Context, clientOrderUUID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	return p.placeLimitOrder(ctx, clientOrderUUID, "buy", size, price)
}

func (p *Product) LimitSell(ctx context.Context, clientOrderUUID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	return p.placeLimitOrder(ctx, clientOrderUUID, "sell", size, price)
}

func (p *Product) placeLimitOrder(ctx context.Context, clientOrderUUID uuid.UUID, side string, size, price decimal.Decimal) (_ exchange.Order, status error) {
	now := time.Now().UnixMilli()
	info := orderPlacementInfo{
		RequestTimeMilli: now,
		Side:             side,
		Price:            price,
		Qty:              size,
		PriceType:        "LIMIT",
		OrderTerm:        "GOOD_UNTIL_CANCEL",
	}
	cstatus, loaded := p.clientIDStatusMap.LoadOrStore(clientOrderUUID, &clientIDStatus{placement: info})
	cstatus.mu.Lock()
	defer cstatus.mu.Unlock()

	// Deduplicate: if this UUID was already submitted, return the cached result.
	if loaded {
		if cstatus.err != nil {
			return nil, cstatus.err
		}
		return cstatus.order, nil
	}
	defer func() { cstatus.err = status }()

	// Persist order parameters with ServerOrderID=0 before calling PlaceOrder.
	// If the process crashes after PlaceOrder succeeds but before we store the
	// server order ID, the next startup can still find the order by matching on
	// (symbol, side, price, qty, time) since E*TRADE omits clientOrderId from
	// list responses.
	p.dbSetEntry(ctx, clientOrderUUID, orderEntry{Placement: info})

	// E*TRADE requires a numeric clientOrderId; generate a unique one for this
	// call only. It is not stored or used for any subsequent lookup.
	etradeClientID := strconv.FormatInt(p.etradeOrderID.Add(1), 10)
	orderID, err := p.client.PlaceLimitOrder(ctx, p.symbol, side, size, price,
		etradeClientID, info.OrderTerm)
	if err != nil {
		// PlaceOrder failed; the order may or may not have been created on
		// E*TRADE. goCancelFailedCreates will resolve this and clean up the DB
		// entry once the outcome is known.
		p.failedCreatesCh <- clientOrderUUID
		return nil, err
	}

	// Update the DB entry with the server order ID now that placement succeeded.
	p.dbSetEntry(ctx, clientOrderUUID, orderEntry{ServerOrderID: orderID, Placement: info})
	p.serverIDMap.Store(orderID, clientOrderUUID)

	// Build a partial order with what we know immediately. It will be updated
	// when goWatchOrderUpdates receives a full response from the polling loop.
	order := &internal.Order{
		OrderID:    orderID,
		ClientUUID: clientOrderUUID,
		Symbol:     p.symbol,
		Side:       side,
		Status:     "OPEN",
		LimitPrice: price,
		OrderedQty: size,
		PlacedTimeMilli: now,
	}
	cstatus.order = order

	// Start tracking the order so goRefreshOrders detects fills/cancels.
	p.client.TrackOrder(orderID)

	return order, nil
}

func (p *Product) Get(ctx context.Context, serverID string) (exchange.OrderDetail, error) {
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	order, err := p.client.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	// Restore ClientUUID from the serverIDMap. E*TRADE's single-order GET
	// response omits clientOrderId, so we cannot rely on the counterIDMap
	// (which is keyed by counterID parsed from that field).
	if uid, ok := p.serverIDMap.Load(orderID); ok {
		order.ClientUUID = uid
	}
	return order, nil
}

func (p *Product) Cancel(ctx context.Context, serverID string) error {
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	return p.client.CancelOrder(ctx, orderID)
}

// goWatchOrderUpdates subscribes to the per-symbol order topic and updates
// clientIDStatusMap whenever an order we placed receives a new state. When an
// order reaches a terminal state, it removes the counterID entry from the DB.
func (p *Product) goWatchOrderUpdates(ctx context.Context) {
	defer p.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goWatchOrderUpdates", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	sub, err := topic.Subscribe(p.client.getSymbolOrdersTopic(p.symbol), 0, true)
	if err != nil {
		slog.Error("etrade: could not subscribe to symbol order topic", "symbol", p.symbol, "err", err)
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

		uid, ok := p.serverIDMap.Load(order.OrderID)
		if !ok {
			continue // placed outside this session
		}

		cstatus, ok := p.clientIDStatusMap.Load(uid)
		if !ok {
			continue
		}
		// Clone before setting ClientUUID to avoid mutating the shared topic
		// pointer while GetOrderUpdates transform may be copying it concurrently.
		owned := *order
		owned.ClientUUID = uid
		cstatus.mu.Lock()
		cstatus.order = &owned
		done := order.IsDone()
		cstatus.mu.Unlock()

		if done {
			p.dbDeleteEntry(ctx, uid)
		}
	}
}

// goCancelFailedCreates handles orders whose PlaceOrder call returned an error.
// It scans open orders by (symbol, side, price, qty, time) to determine if the
// order was actually created despite the error, and cancels it if so.
// If the order is not found in the open list, it assumes the order was not
// created and removes the persisted DB entry. If found, it updates the DB entry
// with the server order ID and cancels; goWatchOrderUpdates will remove
// the entry when the CANCELLED update arrives.
func (p *Product) goCancelFailedCreates(ctx context.Context) {
	defer p.wg.Done()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("etrade: CAUGHT PANIC in goCancelFailedCreates", "panic", r)
			slog.Error(string(debug.Stack()))
			panic(r)
		}
	}()

	retryMap := make(map[uuid.UUID]time.Time)
	retryBackoff := 0

	for {
		// If any retries are due, re-inject one into the channel.
		var retryCh chan uuid.UUID
		var retryUID uuid.UUID
		if len(retryMap) > 0 {
			now := time.Now()
			for uid, at := range retryMap {
				if !at.After(now) {
					retryUID, retryCh = uid, p.failedCreatesCh
					break
				}
			}
		}

		select {
		case <-ctx.Done():
			return

		case retryCh <- retryUID:
			delete(retryMap, retryUID)

		case uid := <-p.failedCreatesCh:
			cstatus, ok := p.clientIDStatusMap.Load(uid)
			if !ok {
				continue
			}
			cstatus.mu.Lock()
			placement := cstatus.placement
			cstatus.mu.Unlock()

			// Scan open orders for a match. E*TRADE does not return clientOrderId
			// in list responses, so match by (symbol, side, price, qty) placed
			// within 60 seconds of our request.
			openOrders, err := p.client.ListOpenOrders(ctx)
			if err != nil {
				if !os.IsTimeout(err) {
					slog.Error("etrade: could not list open orders in goCancelFailedCreates (will retry)",
						"clientOrderUUID", uid, "err", err, "retryAfter", time.Second<<retryBackoff)
				}
				retryMap[uid] = time.Now().Add(time.Second << retryBackoff)
				retryBackoff = min(retryBackoff+1, 6)
				continue
			}

			found := matchOpenOrder(openOrders, p.symbol, placement)

			if found == nil {
				// Order was not created (or already filled before we checked).
				// Leave cstatus.err set from the original PlaceOrder failure and
				// remove the persisted entry since there is no order to track.
				retryBackoff = 0
				slog.Warn("etrade: failed-create order not found in open orders; assuming not created",
					"clientOrderUUID", uid, "side", placement.Side, "price", placement.Price, "qty", placement.Qty)
				p.dbDeleteEntry(ctx, uid)
				continue
			}

			// Order was created. Update the DB entry with the server order ID so
			// that a crash during cancellation can be recovered on next startup.
			p.dbSetEntry(ctx, uid, orderEntry{ServerOrderID: found.OrderID, Placement: placement})
			p.serverIDMap.Store(found.OrderID, uid)

			// Cancel the order and start tracking it so goRefreshOrders polls for
			// the final CANCELLED state; goWatchOrderUpdates removes the DB entry
			// when that update arrives.
			p.client.TrackOrder(found.OrderID)
			if err := p.client.CancelOrder(ctx, found.OrderID); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					slog.Error("etrade: could not cancel failed-create order (will retry)",
						"clientOrderUUID", uid, "orderID", found.OrderID, "err", err,
						"retryAfter", time.Second<<retryBackoff)
					retryMap[uid] = time.Now().Add(time.Second << retryBackoff)
					retryBackoff = min(retryBackoff+1, 6)
					continue
				}
				// os.ErrNotExist means the order disappeared (filled or already
				// cancelled). Remove the entry since the order is conclusively done.
				p.dbDeleteEntry(ctx, uid)
			}

			retryBackoff = 0
			found.ClientUUID = uid
			cstatus.mu.Lock()
			cstatus.order = found
			cstatus.mu.Unlock()
		}
	}
}
