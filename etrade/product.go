// Copyright (c) 2026 Deepak Vankadaru

package etrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/etrade/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/visvasity/topic"
)

// orderEntry is the value persisted in the database for each order we place,
// keyed by the client order UUID. ServerOrderID is 0 until PlaceOrder returns
// successfully. Placement is written before PlaceOrder is called so that if
// the process crashes before the server order ID is known, startup
// reconciliation can match the order in the open-orders list by parameters.
type orderEntry struct {
	ServerOrderID int64
	AccountID     string
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

	// account2serverIDMap maps E*TRADE server order ID → client order UUID, per
	// account. Used by Get() to restore ClientUUID without relying on the
	// clientOrderId field, which E*TRADE omits from single-order GET responses.
	// It is written both at startup (reconcile loop, concurrently with the order
	// goroutines) and at runtime, so the outer map is a thread-safe syncmap; use
	// accountOrders() to get (lazily creating) an account's inner map.
	account2serverIDMap syncmap.Map[string, *syncmap.Map[int64, uuid.UUID]]

	// failedCreatesCh receives UUIDs of orders whose PlaceOrder call returned
	// an error. goCancelFailedCreates scans open orders to find and cancel any
	// that were created despite the error.
	failedCreatesCh chan uuid.UUID

	// productAckReason when non-empty indicates that product should be paused
	// because it needs user acknowledgment and potential job adjustments. The
	// string value holds the reason.
	productAckReason string
	productInfo      *internal.Quote
}

// clientOrderKeyPrefix returns the DB key prefix for all client orders of this symbol.
func (p *Product) clientOrderKeyPrefix() string {
	return fmt.Sprintf("/etrade/symbol/%s/clientorder", p.symbol)
}

// clientOrderKey returns the DB key for a client order UUID.
func (p *Product) clientOrderKey(clientOrderUUID uuid.UUID) string {
	return p.clientOrderKeyPrefix() + "/" + clientOrderUUID.String()
}

// accountOrders returns the per-account server-order-ID → client-UUID map,
// creating it on first use. Safe for concurrent use.
func (p *Product) accountOrders(accountID string) *syncmap.Map[int64, uuid.UUID] {
	m, _ := p.account2serverIDMap.LoadOrStore(accountID, new(syncmap.Map[int64, uuid.UUID]))
	return m
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
func (p *Product) dbSetEntry(ctx context.Context, clientOrderUUID uuid.UUID, entry *orderEntry) error {
	key := p.clientOrderKey(clientOrderUUID)
	if err := kvutil.SetDB(ctx, p.db, key, entry); err != nil {
		return fmt.Errorf("etrade: could not persist order entry for %s/%s: %w", p.symbol, clientOrderUUID, err)
	}
	return nil
}

// dbDeleteEntry removes an orderEntry from the database.
func (p *Product) dbDeleteEntry(ctx context.Context, clientOrderUUID uuid.UUID) error {
	key := p.clientOrderKey(clientOrderUUID)
	err := kv.WithReadWriter(ctx, p.db, func(ctx context.Context, rw kv.ReadWriter) error {
		return rw.Delete(ctx, key)
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("etrade: could not delete order entry for %s/%s: %w", p.symbol, clientOrderUUID, err)
	}
	return nil
}

// NewProduct creates a Product for the given equity symbol. It registers the
// symbol for price polling, loads any persisted counterID entries, starts
// background goroutines, and then reconciles open orders from the previous
// session as the last step so that goWatchOrderUpdates is already subscribed
// when goRefreshOrders publishes any recovered order updates.
func NewProduct(ctx context.Context, db kv.Database, client *Client, symbol string) (*Product, error) {
	// Fetch and watch the symbol information and validate it against the last
	// known info. If there is any corporate action, like stock-split detected,
	// then we should mark this product for user acknowledgment.
	newInfo, err := client.WatchSymbol(ctx, symbol)
	if err != nil {
		return nil, err
	}
	pinfoKey := filepath.Join("/etrade/product-info", symbol)
	oldInfo, err := kvutil.GetDB[internal.Quote](ctx, db, pinfoKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := kvutil.SetDB(ctx, db, pinfoKey, newInfo); err != nil {
			return nil, err
		}
		oldInfo = newInfo
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	p := &Product{
		lifeCtx:             lifeCtx,
		lifeCancel:          lifeCancel,
		db:                  db,
		client:              client,
		symbol:              symbol,
		failedCreatesCh:     make(chan uuid.UUID, 100),
		productInfo:         oldInfo,
		productAckReason:    checkProductNeedsAck(oldInfo, newInfo),
	}
	p.etradeOrderID.Store(time.Now().UnixNano())

	creds := client.creds.Load()
	for _, a := range creds.ActiveAccounts() {
		p.account2serverIDMap.Store(a.AccountID, new(syncmap.Map[int64, uuid.UUID]))
	}

	// Load persisted order entries from the previous session and collect them
	// for reconciliation after goroutines start.
	type pendingEntry struct {
		uid   uuid.UUID
		entry *orderEntry
	}
	var pending []*pendingEntry
	cb := func(ctx context.Context, _ kv.Reader, key string, e *orderEntry) error {
		uid, err := p.clientOrderUUIDFromKey(key)
		if err != nil {
			return err
		}
		if e.AccountID == "" {
			return fmt.Errorf("etrade: database has an order status entry without account id")
		}
		if _, ok := p.account2serverIDMap.Load(e.AccountID); !ok {
			slog.Warn("etrade: database has an order status with unrecognized account id", "account-id", e.AccountID)
		}
		if e.ServerOrderID != 0 && e.AccountID != "" {
			p.accountOrders(e.AccountID).Store(e.ServerOrderID, uid)
		}
		pending = append(pending, &pendingEntry{uid, e})
		return nil
	}
	begin, end := kvutil.PathRange(p.clientOrderKeyPrefix())
	if err := kvutil.AscendDB(ctx, db, begin, end, cb); err != nil {
		lifeCancel(err)
		return nil, fmt.Errorf("etrade: could not load persisted counterID entries for %s: %w", symbol, err)
	}

	// Currently open orders are fetched once and cached across iterations.
	var openOrders []*internal.Order
	if len(pending) != 0 {
		for _, a := range creds.ActiveAccounts() {
			orders, err := p.client.ListOpenOrders(ctx, a.AccountID)
			if err != nil {
				slog.Error("etrade: could not list open orders during startup", "symbol", symbol, "account-id", a.AccountID, "err", err)
				lifeCancel(err)
				return nil, err
			}
			for _, v := range orders {
				if v.Symbol == symbol {
					openOrders = append(openOrders, v)
				}
			}
		}
		slog.Info("etrade: open orders found for symbol at startup", "symbol", symbol, "count", len(openOrders))
	}

	// Start goroutines before reconciling so that goWatchOrderUpdates is
	// subscribed to the symbol orders topic before goRefreshOrders (via
	// TrackOrder) begins publishing recovered order updates.
	p.wg.Add(2)
	go p.goWatchOrderUpdates(p.lifeCtx)
	go p.goCancelFailedCreates(p.lifeCtx)

	// Reconcile each persisted entry as the last step. For entries with a known
	// server order ID, update the current state from E*TRADE. For entries with
	// ServerOrderID==0 (PlaceOrder outcome was unknown), update the server order
	// id.
	for _, pe := range pending {
		uid := pe.uid
		entry := pe.entry

		unknownAccountID := false
		if _, ok := p.account2serverIDMap.Load(entry.AccountID); !ok {
			slog.Warn("etrade: account id from one of the pending orders is not found in credentials", "account-id", entry.AccountID, "symbol", symbol)
			unknownAccountID = true
		}

		if entry.ServerOrderID == 0 {
			// We don't have server-order-id, which means PlaceOrder may not have
			// completed before the previous session ended.
			if len(openOrders) == 0 {
				// ListOpenOrders unavailable; defer to goCancelFailedCreates.
				p.clientIDStatusMap.Store(uid, &clientIDStatus{accountID: entry.AccountID, placement: entry.Placement})
				p.failedCreatesCh <- uid
				continue
			}
			found := matchOpenOrder(openOrders, entry.AccountID, symbol, entry.Placement)
			if found == nil {
				// Not in open orders: either never created, or filled/cancelled
				// before we could check. Remove the entry.
				slog.Warn("etrade: startup: order with unknown server ID not found in open orders; removing", "symbol", symbol, "account-id", entry.AccountID, "uid", uid)
				if err := p.dbDeleteEntry(ctx, uid); err != nil {
					lifeCancel(err)
					return nil, err
				}
				continue
			}
			// Found in open orders. Persist the server order ID now that we have it.
			entry.ServerOrderID = found.OrderID
			if err := p.dbSetEntry(ctx, uid, entry); err != nil {
				lifeCancel(err)
				return nil, err
			}

			p.accountOrders(entry.AccountID).Store(found.OrderID, uid)
			found.ClientUUID = uid
			p.clientIDStatusMap.Store(uid, &clientIDStatus{accountID: entry.AccountID, order: found})
			if !unknownAccountID {
				p.client.TrackOrder(entry.AccountID, found.OrderID)
			}
			continue
		}

		// Fetch current order state using the persisted server order ID.
		order, err := p.client.GetOrder(ctx, entry.AccountID, entry.ServerOrderID)
		if err != nil {
			slog.Warn("etrade: startup: could not fetch order state; will rely on polling to recover", "symbol", symbol, "uid", uid, "serverOrderID", entry.ServerOrderID, "err", err)
			// Leave the entry in the DB. Re-register for tracking so polling
			// picks it up and the topic update eventually reaches goWatchOrderUpdates.
			p.client.TrackOrder(entry.AccountID, entry.ServerOrderID)
			continue
		}
		p.accountOrders(entry.AccountID).Store(entry.ServerOrderID, uid)
		order.ClientUUID = uid
		p.clientIDStatusMap.Store(uid, &clientIDStatus{accountID: entry.AccountID, order: order})
		if order.IsDone() {
			if err := p.dbDeleteEntry(ctx, uid); err != nil {
				lifeCancel(err)
				return nil, err
			}
		} else {
			p.client.TrackOrder(entry.AccountID, entry.ServerOrderID)
		}
	}

	return p, nil
}

func (p *Product) Close() error {
	p.lifeCancel(os.ErrClosed)
	p.wg.Wait()
	return nil
}

func (p *Product) GetOrderUpdates(accountID string) (*topic.Receiver[exchange.OrderUpdate], error) {
	fn := func(o *internal.Order) exchange.OrderUpdate {
		if o.AccountID != accountID {
			return nil
		}
		if o.ClientUUID != uuid.Nil {
			return o
		}
		uid, ok := p.accountOrders(accountID).Load(o.OrderID)
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

// sleepUntilExtendedHoursOpen waits until 7am ET (when E*TRADE extended hours
// trading opens). Called when extended hours are closed and we need to retry.
func (p *Product) sleepUntilExtendedHoursOpen(ctx context.Context) error {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return fmt.Errorf("could not load NY timezone: %w", err)
	}
	now := time.Now().In(loc)
	year, month, day := now.Date()
	nextOpen := time.Date(year, month, day, 7, 0, 0, 0, loc) // 7am ET today
	if !now.Before(nextOpen) {
		nextOpen = nextOpen.Add(24 * time.Hour) // Already at or past 7am, wait until tomorrow
	}
	// Skip weekends: if nextOpen lands on Saturday or Sunday, advance to Monday.
	switch nextOpen.Weekday() {
	case time.Saturday:
		nextOpen = nextOpen.Add(48 * time.Hour)
	case time.Sunday:
		nextOpen = nextOpen.Add(24 * time.Hour)
	}
	waitDur := nextOpen.Sub(now)
	slog.Warn("etrade: extended hours closed, sleeping until 7am ET", "wake-at", nextOpen, "wait", waitDur)
	select {
	case <-time.After(waitDur):
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-p.lifeCtx.Done():
		return context.Cause(p.lifeCtx)
	}
}

// roundLimitPrice rounds a limit price to the precision E*TRADE requires:
// 2 decimal places for prices >= $1, 4 decimal places for prices < $1.
// Buy orders truncate (never overpay); sell orders ceiling (never undersell).
func roundLimitPrice(side string, price decimal.Decimal) decimal.Decimal {
	places := int32(2)
	if price.LessThan(decimal.NewFromInt(1)) {
		places = 4
	}
	if strings.EqualFold(side, "buy") {
		return price.Truncate(places)
	}
	truncated := price.Truncate(places)
	if truncated.Equal(price) {
		return price
	}
	return truncated.Add(decimal.New(1, -places))
}

func checkProductNeedsAck(oldInfo, newInfo *internal.Quote) string {
	if newInfo.SharesOutstanding != oldInfo.SharesOutstanding {
		change := float64(newInfo.SharesOutstanding-oldInfo.SharesOutstanding) / float64(oldInfo.SharesOutstanding) * 100.0
		return fmt.Sprintf("Number of outstanding stock for %s changed from %d to %d (by %f%%)", newInfo.Symbol, oldInfo.SharesOutstanding, newInfo.SharesOutstanding, change)
	}
	return ""
}

func (p *Product) PlaceLimitOrder(ctx context.Context, accountID string, clientOrderUUID uuid.UUID, side string, size, price decimal.Decimal, taxLotIDs []gobs.TaxLotID) (_ exchange.Order, status error) {
	if err := p.checkProductNeedsAck(ctx); err != nil {
		return nil, err
	}
	creds := p.client.creds.Load()
	if _, err := creds.GetAccountIDKey(accountID); err != nil {
		return nil, err
	}

	var plots []*placeOrderLot
	if len(taxLotIDs) > 0 {
		if !strings.EqualFold(side, "sell") {
			return nil, fmt.Errorf("etrade: tax lots are supported only on sell side")
		}
		lots, err := p.client.ResolveTaxLotIDs(ctx, taxLotIDs)
		if err != nil {
			return nil, err
		}
		var sum decimal.Decimal
		for _, l := range lots {
			plots = append(plots, &placeOrderLot{ID: l.PositionLotID, Size: l.RemainingQty})
			sum = sum.Add(l.RemainingQty)
		}
		if !sum.Equal(size) {
			return nil, fmt.Errorf("etrade: sell with lots size %s did not match lots quantity %s", size, sum)
		}
	}

	now := time.Now().UnixMilli()
	price = roundLimitPrice(side, price)
	info := orderPlacementInfo{
		RequestTimeMilli: now,
		Side:             side,
		Price:            price,
		Qty:              size,
		PriceType:        "LIMIT",
		OrderTerm:        "GOOD_FOR_DAY",
		MarketSession:    "EXTENDED",
	}
	cstatus, loaded := p.clientIDStatusMap.LoadOrStore(clientOrderUUID, &clientIDStatus{accountID: accountID, placement: info})
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
	if err := p.dbSetEntry(ctx, clientOrderUUID, &orderEntry{AccountID: accountID, Placement: info}); err != nil {
		return nil, err
	}

	// E*TRADE requires a numeric clientOrderId; generate a unique one for this
	// call only. It is not stored or used for any subsequent lookup.
	var orderID int64
	var err error
	for {
		etradeClientID := strconv.FormatInt(p.etradeOrderID.Add(1), 10)
		orderID, err = p.client.PlaceLimitOrder(ctx, accountID, p.symbol, side, size, price,
			etradeClientID, info.OrderTerm, info.MarketSession, plots)
		if err == nil {
			break
		}
		if errors.Is(err, ErrExtendedHoursClosed) {
			// Extended hours window is closed. Sleep until 7am ET and retry.
			if sleepErr := p.sleepUntilExtendedHoursOpen(ctx); sleepErr != nil {
				// No order was created (E*TRADE rejected at preview). Delete
				// the DB entry directly rather than going through failedCreatesCh.
				p.dbDeleteEntry(p.lifeCtx, clientOrderUUID)
				return nil, sleepErr
			}
			continue
		}
		// Other error: order may or may not have been created on E*TRADE.
		// goCancelFailedCreates will resolve this and clean up the DB entry.
		p.failedCreatesCh <- clientOrderUUID
		return nil, err
	}

	// Update the DB entry with the server order ID now that placement succeeded.
	// Retry on p.lifeCtx, not ctx: the order exists on E*TRADE so we must not
	// give up persisting its server ID just because the caller cancelled. A crash
	// without this entry would leave the order untrackable on restart.
	for {
		err := p.dbSetEntry(p.lifeCtx, clientOrderUUID, &orderEntry{AccountID: accountID, ServerOrderID: orderID, Placement: info})
		if err == nil {
			break
		}
		slog.Error("etrade: could not persist server order ID after placement (will retry)",
			"symbol", p.symbol, "clientOrderUUID", clientOrderUUID, "orderID", orderID, "err", err)
		select {
		case <-p.lifeCtx.Done():
			return nil, fmt.Errorf("etrade: product closing before server order ID could be persisted: %w", p.lifeCtx.Err())
		case <-time.After(time.Second):
		}
	}
	p.accountOrders(accountID).Store(orderID, clientOrderUUID)

	// Build a partial order with what we know immediately. It will be updated
	// when goWatchOrderUpdates receives a full response from the polling loop.
	order := &internal.Order{
		AccountID:       accountID,
		OrderID:         orderID,
		ClientUUID:      clientOrderUUID,
		Symbol:          p.symbol,
		Side:            side,
		Status:          "OPEN",
		LimitPrice:      price,
		OrderedQty:      size,
		PlacedTimeMilli: now,
	}
	cstatus.order = order

	// Start tracking the order so goRefreshOrders detects fills/cancels.
	p.client.TrackOrder(accountID, orderID)

	return order, nil
}

func (p *Product) Get(ctx context.Context, accountID, serverID string) (exchange.OrderDetail, error) {
	creds := p.client.creds.Load()
	if _, err := creds.GetAccountIDKey(accountID); err != nil {
		return nil, err
	}
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	order, err := p.client.GetOrder(ctx, accountID, orderID)
	if err != nil {
		return nil, err
	}
	// Restore ClientUUID from the serverIDMap. E*TRADE's single-order GET
	// response omits clientOrderId, so we cannot rely on the counterIDMap
	// (which is keyed by counterID parsed from that field).
	if uid, ok := p.accountOrders(accountID).Load(orderID); ok {
		order.ClientUUID = uid
	}
	return order, nil
}

func (p *Product) Cancel(ctx context.Context, accountID, serverID string) error {
	creds := p.client.creds.Load()
	if _, err := creds.GetAccountIDKey(accountID); err != nil {
		return err
	}
	orderID, err := strconv.ParseInt(serverID, 10, 64)
	if err != nil {
		return fmt.Errorf("etrade: invalid server order id %q: %w", serverID, err)
	}
	return p.client.CancelOrder(ctx, accountID, orderID)
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

	updatesCh, err := topic.ReceiveCh(sub)
	if err != nil {
		slog.Error("etrade: could not create receive channel from topic", "symbol", p.symbol, "err", err)
		return
	}

	for ctx.Err() == nil {
		creds := p.client.creds.Load()

		select {
		case <-p.lifeCtx.Done():
			return
		case order := <-updatesCh:
			if _, err := creds.GetAccountIDKey(order.AccountID); err != nil {
				slog.Error("etrade: invalid/unrecognized etrade account id", "account-id", order.AccountID, "err", err)
				continue
			}
			uid, ok := p.accountOrders(order.AccountID).Load(order.OrderID)
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
				if err := p.dbDeleteEntry(ctx, uid); err != nil {
					slog.Warn("etrade: could not delete completed order entry", "symbol", p.symbol, "account-id", order.AccountID, "uid", uid, "err", err)
				}
			}
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
			creds := p.client.creds.Load()
			if _, err := creds.GetAccountIDKey(cstatus.accountID); err != nil {
				slog.Error("etrade: invalid/unrecognized etrade account id", "account-id", cstatus.accountID, "err", err)
				continue
			}
			openOrders, err := p.client.ListOpenOrders(ctx, cstatus.accountID)
			if err != nil {
				if !os.IsTimeout(err) {
					slog.Error("etrade: could not list open orders in goCancelFailedCreates (will retry)",
						"clientOrderUUID", uid, "err", err, "retryAfter", time.Second<<retryBackoff)
				}
				retryMap[uid] = time.Now().Add(time.Second << retryBackoff)
				retryBackoff = min(retryBackoff+1, 6)
				continue
			}

			found := matchOpenOrder(openOrders, cstatus.accountID, p.symbol, placement)

			if found == nil {
				// Order was not created (or already filled before we checked).
				// Leave cstatus.err set from the original PlaceOrder failure and
				// remove the persisted entry since there is no order to track.
				retryBackoff = 0
				slog.Warn("etrade: failed-create order not found in open orders; assuming not created",
					"clientOrderUUID", uid, "side", placement.Side, "price", placement.Price, "qty", placement.Qty)
				if err := p.dbDeleteEntry(ctx, uid); err != nil {
					slog.Error("etrade: could not delete unrecovered order entry (will retry)",
						"clientOrderUUID", uid, "err", err, "retryAfter", time.Second<<retryBackoff)
					retryMap[uid] = time.Now().Add(time.Second << retryBackoff)
					retryBackoff = min(retryBackoff+1, 6)
					continue
				}
				continue
			}

			// Order was created. Update the DB entry with the server order ID so
			// that a crash during cancellation can be recovered on next startup.
			if err := p.dbSetEntry(ctx, uid, &orderEntry{AccountID: cstatus.accountID, ServerOrderID: found.OrderID, Placement: placement}); err != nil {
				slog.Error("etrade: could not persist server order ID for failed-create (will retry)",
					"clientOrderUUID", uid, "orderID", found.OrderID, "err", err, "retryAfter", time.Second<<retryBackoff)
				retryMap[uid] = time.Now().Add(time.Second << retryBackoff)
				retryBackoff = min(retryBackoff+1, 6)
				continue
			}
			p.accountOrders(cstatus.accountID).Store(found.OrderID, uid)

			// Cancel the order and start tracking it so goRefreshOrders polls for
			// the final CANCELLED state; goWatchOrderUpdates removes the DB entry
			// when that update arrives.
			p.client.TrackOrder(cstatus.accountID, found.OrderID)
			if err := p.client.CancelOrder(ctx, cstatus.accountID, found.OrderID); err != nil {
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
				if err := p.dbDeleteEntry(ctx, uid); err != nil {
					slog.Warn("etrade: could not delete disappeared order entry", "clientOrderUUID", uid, "err", err)
				}
			}

			retryBackoff = 0
			found.ClientUUID = uid
			cstatus.mu.Lock()
			cstatus.order = found
			cstatus.mu.Unlock()
		}
	}
}

func (p *Product) checkProductNeedsAck(ctx context.Context) error {
	if p.productAckReason != "" {
		return fmt.Errorf("etrade: product %s needs acknowledgment: %w", p.symbol, exchange.ErrNeedsAck)
	}
	newInfo, ok := p.client.getSymbolInfo(p.symbol)
	if !ok {
		return fmt.Errorf("etrade: could not find product info for %q in the client", p.symbol)
	}
	reason := checkProductNeedsAck(p.productInfo, newInfo)
	if reason == "" {
		return nil
	}
	// pinfoKey := filepath.Join("/etrade/product-info", p.symbol)
	// if err := kvutil.SetDB(ctx, p.db, pinfoKey, newInfo); err != nil {
	// 	slog.Error("etrade: could not save product information change", "product", p.symbol)
	// 	return err
	// }
	p.productInfo = newInfo
	p.productAckReason = reason
	if p.client.opts.Messenger != nil {
		p.client.opts.Messenger.SendMessage(ctx, time.Now(), "E*TRADE: Product %s is *paused*: %s", p.symbol, reason)
	}
	slog.Info("E*TRADE: "+reason, "symbol", p.symbol)
	return fmt.Errorf("etrade: product %s needs acknowledgment: %w", p.symbol, exchange.ErrNeedsAck)
}

func (p *Product) AcknowledeReason() string {
	return p.productAckReason
}

func (p *Product) AcknowledgeProduct(ctx context.Context, reason string) error {
	p.productAckReason = "" // FIXME: This must be atomic
	return nil
}

func (p *Product) ResolveTaxLots(ctx context.Context, accountID string, serverIDs []string) ([]gobs.TaxLotID, decimal.Decimal, error) {
	var sum decimal.Decimal
	oids := make([]int64, len(serverIDs))
	for i, v := range serverIDs {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, sum, err
		}
		oids[i] = id
	}
	lots, err := p.client.ResolveOrderLots(ctx, accountID, p.symbol, oids)
	if err != nil {
		return nil, sum, err
	}
	if len(lots) == 0 {
		slog.Warn(fmt.Sprintf("etrade: orders %v are resolved to zero lots", oids))
		return nil, sum, nil
	}
	var taxLots []gobs.TaxLotID
	for _, lot := range lots {
		js, _ := json.Marshal(lot)
		log.Printf("%s", js)

		sum = sum.Add(lot.RemainingQty)
		taxLotID, err := p.client.makeTaxLotID(lot.AccountID, lot.Symbol, lot.LotsDetails, lot.OrderNo)

		if err != nil {
			return nil, sum, nil
		}
		taxLots = append(taxLots, taxLotID)
	}
	return taxLots, sum, nil
}
