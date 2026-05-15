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

	// counterID is the numeric E*TRADE clientOrderId we assigned and sent in
	// PlaceOrder. Used by goCancelFailedCreates to find the order if placement
	// timed out.
	counterID int64

	timestampMilli int64
}

func (v *clientIDStatus) isDoneLocked() bool {
	return v.order != nil && v.order.IsDone()
}

// counterIDEntry is the value persisted in the database for each order we
// place. It maps the numeric E*TRADE clientOrderId to the client order UUID
// and the server order ID. ServerOrderID is 0 until PlaceOrder returns
// successfully, allowing crash recovery even when the outcome is unknown.
type counterIDEntry struct {
	ClientOrderUUID uuid.UUID
	ServerOrderID   int64
}

// Product implements exchange.Product for a single E*TRADE equity symbol.
type Product struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	db     kv.Database
	client *Client
	symbol string

	// nextCounterID generates unique numeric E*TRADE clientOrderIds. E*TRADE
	// only accepts digits in this field, so UUID encoding is not possible.
	nextCounterID atomic.Int64

	// clientIDStatusMap is keyed by the client order UUID and tracks order state
	// for deduplication and status queries within a session.
	clientIDStatusMap syncmap.Map[uuid.UUID, *clientIDStatus]

	// counterIDMap maps E*TRADE numeric clientOrderId → client order UUID,
	// allowing order updates received from the polling goroutines to be matched
	// back to the originating clientIDStatus.
	counterIDMap syncmap.Map[int64, uuid.UUID]

	// failedCreatesCh receives UUIDs of orders whose PlaceOrder call returned
	// an error. goCancelFailedCreates scans open orders to find and cancel any
	// that were created despite the error.
	failedCreatesCh chan uuid.UUID
}

var _ exchange.Product = &Product{}

// counterIDKey returns the DB key for a given E*TRADE numeric clientOrderId.
func (p *Product) counterIDKey(counterID int64) string {
	return fmt.Sprintf("/etrade/symbol/%s/counterid/%d", p.symbol, counterID)
}

// dbSetEntry persists a counterIDEntry to the database.
func (p *Product) dbSetEntry(ctx context.Context, counterID int64, entry counterIDEntry) {
	key := p.counterIDKey(counterID)
	if err := kvutil.SetDB[counterIDEntry](ctx, p.db, key, &entry); err != nil {
		slog.Warn("etrade: could not persist counterID entry", "symbol", p.symbol, "counterID", counterID, "err", err)
	}
}

// dbDeleteEntry removes a counterIDEntry from the database.
func (p *Product) dbDeleteEntry(ctx context.Context, counterID int64) {
	key := p.counterIDKey(counterID)
	err := kv.WithReadWriter(ctx, p.db, func(ctx context.Context, rw kv.ReadWriter) error {
		return rw.Delete(ctx, key)
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("etrade: could not delete counterID entry", "symbol", p.symbol, "counterID", counterID, "err", err)
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
	p.nextCounterID.Store(time.Now().UnixNano())

	// Load persisted counterID entries from the previous session. Populate
	// counterIDMap now; collect entries for reconciliation after goroutines start.
	type pendingEntry struct {
		counterID int64
		entry     counterIDEntry
	}
	var pending []pendingEntry
	begin, end := kvutil.PathRange(fmt.Sprintf("/etrade/symbol/%s/counterid", symbol))
	loadErr := kvutil.AscendDB[counterIDEntry](ctx, db, begin, end, func(ctx context.Context, _ kv.Reader, key string, e *counterIDEntry) error {
		// Key format: /etrade/symbol/<symbol>/counterid/<counterID>
		parts := strings.Split(key, "/")
		counterID, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			return fmt.Errorf("etrade: invalid counterID key %q: %w", key, err)
		}
		p.counterIDMap.Store(counterID, e.ClientOrderUUID)
		pending = append(pending, pendingEntry{counterID, *e})
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
		cid := pe.counterID
		entry := pe.entry
		uid := entry.ClientOrderUUID

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
				p.clientIDStatusMap.Store(uid, &clientIDStatus{counterID: cid})
				p.failedCreatesCh <- uid
				continue
			}
			// Check open orders to see if the order was actually created.
			counterIDStr := strconv.FormatInt(cid, 10)
			var found *internal.Order
			for _, o := range openOrders {
				if o.Symbol == symbol && o.ClientOrderID == counterIDStr {
					found = o
					break
				}
			}
			if found == nil {
				// Not in open orders: either never created, or filled/cancelled
				// before we could check. Remove the entry; the caller's error
				// from the original PlaceOrder failure remains authoritative.
				slog.Warn("etrade: startup: counterID entry with unknown server order ID not found in open orders; removing",
					"symbol", symbol, "counterID", cid)
				p.counterIDMap.Delete(cid)
				p.dbDeleteEntry(ctx, cid)
				continue
			}
			// Found in open orders. Persist the server order ID now that we have it.
			entry.ServerOrderID = found.OrderID
			p.dbSetEntry(ctx, cid, entry)
			found.ClientUUID = uid
			p.clientIDStatusMap.Store(uid, &clientIDStatus{
				counterID:      cid,
				order:          found,
				timestampMilli: found.PlacedTimeMilli,
			})
			p.client.TrackOrder(found.OrderID)
			continue
		}

		// Fetch current order state using the persisted server order ID.
		order, err := p.client.GetOrder(ctx, entry.ServerOrderID)
		if err != nil {
			slog.Warn("etrade: startup: could not fetch order state; will rely on polling to recover",
				"symbol", symbol, "counterID", cid, "serverOrderID", entry.ServerOrderID, "err", err)
			// Leave the entry in the DB. Re-register for tracking so polling
			// picks it up and the topic update eventually reaches goWatchOrderUpdates.
			p.client.TrackOrder(entry.ServerOrderID)
			continue
		}
		order.ClientUUID = uid
		p.clientIDStatusMap.Store(uid, &clientIDStatus{
			counterID:      cid,
			order:          order,
			timestampMilli: order.PlacedTimeMilli,
		})
		if order.IsDone() {
			p.counterIDMap.Delete(cid)
			p.dbDeleteEntry(ctx, cid)
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
	fn := func(o *internal.Order) exchange.OrderUpdate { return o }
	return topic.SubscribeFunc(p.client.getSymbolOrdersTopic(p.symbol), fn, 0, true)
}

func (p *Product) GetPriceUpdates() (*topic.Receiver[exchange.PriceUpdate], error) {
	fn := func(q *internal.Quote) exchange.PriceUpdate { return q }
	return topic.SubscribeFunc(p.client.getSymbolPriceTopic(p.symbol), fn, 1, true)
}

func (p *Product) LimitBuy(ctx context.Context, clientOrderUUID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	return p.placeOrder(ctx, clientOrderUUID, "buy", size, price)
}

func (p *Product) LimitSell(ctx context.Context, clientOrderUUID uuid.UUID, size, price decimal.Decimal) (_ exchange.Order, status error) {
	return p.placeOrder(ctx, clientOrderUUID, "sell", size, price)
}

func (p *Product) placeOrder(ctx context.Context, clientOrderUUID uuid.UUID, side string, size, price decimal.Decimal) (_ exchange.Order, status error) {
	counterID := p.nextCounterID.Add(1)
	cstatus, loaded := p.clientIDStatusMap.LoadOrStore(clientOrderUUID, &clientIDStatus{
		counterID:      counterID,
		timestampMilli: time.Now().UnixMilli(),
	})
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

	// Register the counterID → client order UUID mapping in memory and persist
	// it with ServerOrderID=0 before calling PlaceOrder. This ensures that even
	// if PlaceOrder succeeds but the process crashes before we update the entry,
	// the next startup can scan open orders to recover the server order ID.
	p.counterIDMap.Store(cstatus.counterID, clientOrderUUID)
	p.dbSetEntry(ctx, cstatus.counterID, counterIDEntry{
		ClientOrderUUID: clientOrderUUID,
		ServerOrderID:   0,
	})

	orderID, err := p.client.PlaceOrder(ctx, p.symbol, side, size, price,
		strconv.FormatInt(cstatus.counterID, 10), "GOOD_UNTIL_CANCEL")
	if err != nil {
		// PlaceOrder failed; the order may or may not have been created on
		// E*TRADE. goCancelFailedCreates will resolve this and clean up the DB
		// entry once the outcome is known.
		p.failedCreatesCh <- clientOrderUUID
		return nil, err
	}

	// Update the DB entry with the server order ID now that placement succeeded.
	p.dbSetEntry(ctx, cstatus.counterID, counterIDEntry{
		ClientOrderUUID: clientOrderUUID,
		ServerOrderID:   orderID,
	})

	// Build a partial order with what we know immediately. It will be updated
	// when goWatchOrderUpdates receives a full response from the polling loop.
	order := &internal.Order{
		OrderID:         orderID,
		ClientOrderID:   strconv.FormatInt(cstatus.counterID, 10),
		ClientUUID:      clientOrderUUID,
		Symbol:          p.symbol,
		Side:            side,
		Status:          "OPEN",
		LimitPrice:      price,
		OrderedQty:      size,
		PlacedTimeMilli: time.Now().UnixMilli(),
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
	// Restore ClientUUID from the counterIDMap if we placed this order.
	if counterID, parseErr := strconv.ParseInt(order.ClientOrderID, 10, 64); parseErr == nil {
		if uid, ok := p.counterIDMap.Load(counterID); ok {
			order.ClientUUID = uid
		}
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

		// Resolve the client order UUID from the numeric clientOrderId.
		counterID, parseErr := strconv.ParseInt(order.ClientOrderID, 10, 64)
		if parseErr != nil {
			continue // not our order format
		}
		uid, ok := p.counterIDMap.Load(counterID)
		if !ok {
			continue // placed outside this session
		}

		order.ClientUUID = uid

		cstatus, ok := p.clientIDStatusMap.Load(uid)
		if !ok {
			continue
		}
		cstatus.mu.Lock()
		cstatus.order = order
		done := order.IsDone()
		cstatus.mu.Unlock()

		if done {
			p.counterIDMap.Delete(counterID)
			p.dbDeleteEntry(ctx, counterID)
		}
	}
}

// goCancelFailedCreates handles orders whose PlaceOrder call returned an error.
// Because E*TRADE does not support cancel-by-clientOrderId, it scans open
// orders for the matching numeric clientOrderId and cancels by server orderID.
// If the order is not found in the open list, it assumes the order was not
// created and removes the persisted counterID entry. If found, it updates the
// DB entry with the server order ID and cancels; goWatchOrderUpdates will remove
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
			counterID := cstatus.counterID
			counterIDStr := strconv.FormatInt(counterID, 10)
			cstatus.mu.Unlock()

			// Scan open orders for a match on our clientOrderId.
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

			var found *internal.Order
			for _, o := range openOrders {
				if o.Symbol == p.symbol && o.ClientOrderID == counterIDStr {
					found = o
					break
				}
			}

			if found == nil {
				// Order was not created (or already filled before we checked).
				// Leave cstatus.err set from the original PlaceOrder failure and
				// remove the persisted entry since there is no order to track.
				retryBackoff = 0
				slog.Warn("etrade: failed-create order not found in open orders; assuming not created",
					"clientOrderUUID", uid, "counterID", counterIDStr)
				p.counterIDMap.Delete(counterID)
				p.dbDeleteEntry(ctx, counterID)
				continue
			}

			// Order was created. Update the DB entry with the server order ID so
			// that a crash during cancellation can be recovered on next startup.
			p.dbSetEntry(ctx, counterID, counterIDEntry{
				ClientOrderUUID: uid,
				ServerOrderID:   found.OrderID,
			})

			// Cancel the order. goWatchOrderUpdates removes the DB entry when the
			// CANCELLED update arrives via the polling loop.
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
				p.counterIDMap.Delete(counterID)
				p.dbDeleteEntry(ctx, counterID)
			}

			retryBackoff = 0
			found.ClientUUID = uid
			cstatus.mu.Lock()
			cstatus.order = found
			cstatus.mu.Unlock()
		}
	}
}
