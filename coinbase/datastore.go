// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
)

const Keyspace = "/coinbase/"

type Datastore struct {
	db kv.Database

	mu sync.Mutex

	recentlySaved []*internal.Order
}

func NewDatastore(db kv.Database) *Datastore {
	return &Datastore{
		db: db,
	}
}

// ScanFilled runs the callback with selected orders from the datastore.
//
// Orders can be selected for specific product id using a non-empty `productID`
// otherwise orders for all products are selected.
//
// Also, orders can be selected with specific last filled timestamp between
// `begin` and `end` timestamps. When `begin` or `end` timestamps are zero they
// refer to beginning of all timestamps and ending of all timestamps.
func (ds *Datastore) ScanFilled(ctx context.Context, productID string, begin, end time.Time, fn func(*gobs.Order) error) error {
	minKey := path.Join(Keyspace, "filled", "0000-00-00/00")
	if !begin.IsZero() {
		minKey = path.Join(Keyspace, "filled", begin.Format("2006-01-02/15"))
	}
	maxKey := path.Join(Keyspace, "filled", "9999-99-99/99")
	if !end.IsZero() {
		maxKey = path.Join(Keyspace, "filled", end.Format("2006-01-02/15"))
	}

	wrapper := func(ctx context.Context, r kv.Reader, k string, v *gobs.CoinbaseOrders) error {
		for pid, ids := range v.ProductOrderIDsMap {
			if len(productID) > 0 && pid != productID {
				continue
			}
			for _, id := range ids {
				order, err := ds.loadOrderLocked(ctx, r, id)
				if err != nil {
					return fmt.Errorf("could not load order: %w", err)
				}
				if !begin.IsZero() && order.LastFillTime.Time.Before(begin) {
					continue
				}
				if !end.IsZero() && end.Before(order.LastFillTime.Time) {
					continue
				}
				if err := fn(gobOrderFromOrder(order)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return kvutil.AscendDB(ctx, ds.db, minKey, maxKey, wrapper)
}

func (ds *Datastore) LastFilledTime(ctx context.Context) (time.Time, error) {
	minKey := path.Join(Keyspace, "filled", "0000-00-00/00")
	maxKey := path.Join(Keyspace, "filled", "9999-99-99/99")
	key := ""
	ErrStop := errors.New("STOP")
	keyBefore := func(ctx context.Context, r kv.Reader) error {
		it, err := r.Descend(ctx, minKey, maxKey)
		if err != nil {
			return fmt.Errorf("could not create descending iterator: %w", err)
		}
		defer kv.Close(it)

		k, _, err := it.Fetch(ctx, false)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("could not fetch from descending iterator: %w", err)
		}
		key = k
		return ErrStop
	}
	if err := kv.WithReader(ctx, ds.db, keyBefore); err != nil {
		if !errors.Is(err, ErrStop) {
			return time.Time{}, fmt.Errorf("could not determine the largest key: %w", err)
		}
	}
	if len(key) == 0 {
		return time.Time{}, os.ErrNotExist
	}
	str := strings.TrimPrefix(key, path.Join(Keyspace, "filled"))
	var y, m, d, h int
	if _, err := fmt.Sscanf(str, "/%04d-%02d-%02d/%02d", &y, &m, &d, &h); err != nil {
		return time.Time{}, fmt.Errorf("could not scan timestamp fields from %q: %w", str, err)
	}
	return time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC), nil
}

func (ex *Exchange) ListOrders(ctx context.Context, from time.Time, status string) ([]*exchange.Order, error) {
	var orders []*exchange.Order
	rorders, err := ex.listRawOrders(ctx, from, status)
	if err != nil {
		return nil, fmt.Errorf("could not list raw orders: %w", err)
	}
	for _, order := range rorders {
		v := exchangeOrderFromOrder(order)
		ex.dispatchOrder(order.ProductID, v)
		orders = append(orders, v)
	}
	return orders, nil
}

func (ds *Datastore) saveCandles(ctx context.Context, productID string, candles []*internal.Candle) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	cmp := func(a, b *gobs.CoinbaseCandle) int {
		return cmp.Compare(a.UnixTime, b.UnixTime)
	}
	equal := func(a, b *gobs.CoinbaseCandle) bool {
		return a.UnixTime == b.UnixTime
	}

	kmap := make(map[string][]*internal.Candle)
	for _, v := range candles {
		ts := time.Unix(v.Start, 0)
		key := path.Join(Keyspace, "candles", ts.Format("2006-01-02/15"))
		vs := kmap[key]
		kmap[key] = append(vs, v)
	}

	saver := func(ctx context.Context, rw kv.ReadWriter) error {
		for key, candles := range kmap {
			value, err := kvutil.Get[gobs.CoinbaseCandles](ctx, rw, key)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("could not load coinbase candles at %q: %w", key, err)
				}
				value = &gobs.CoinbaseCandles{
					ProductCandlesMap: make(map[string][]*gobs.CoinbaseCandle),
				}
			}
			pcandles := value.ProductCandlesMap[productID]
			for _, c := range candles {
				js, err := json.Marshal(c)
				if err != nil {
					return fmt.Errorf("could not json-marshal candle: %w", err)
				}
				pcandles = append(pcandles, &gobs.CoinbaseCandle{UnixTime: c.Start, Candle: json.RawMessage(js)})
			}
			slices.SortFunc(pcandles, cmp)
			pcandles = slices.CompactFunc(pcandles, equal)
			value.ProductCandlesMap[productID] = pcandles
			if err := kvutil.Set(ctx, rw, key, value); err != nil {
				return fmt.Errorf("could not save candles at key %q: %w", key, err)
			}
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, ds.db, saver); err != nil {
		return err
	}
	return nil
}

// ScanCandles
func (ds *Datastore) ScanCandles(ctx context.Context, productID string, begin, end time.Time, fn func(*gobs.Candle) error) error {
	if len(productID) == 0 {
		return fmt.Errorf("product id cannot be empty")
	}

	minKey := path.Join(Keyspace, "candles", "0000-00-00/00")
	if !begin.IsZero() {
		minKey = path.Join(Keyspace, "candles", begin.Truncate(time.Hour).Format("2006-01-02/15"))
	}
	maxKey := path.Join(Keyspace, "candles", "9999-99-99/99")
	if !end.IsZero() {
		maxKey = path.Join(Keyspace, "candles", end.Truncate(time.Hour).Format("2006-01-02/15"))
	}

	scanner := func(ctx context.Context, r kv.Reader, k string, v *gobs.CoinbaseCandles) error {
		candles, ok := v.ProductCandlesMap[productID]
		if !ok {
			return nil
		}

		for _, c := range candles {
			if !begin.IsZero() && c.UnixTime < begin.Unix() {
				continue
			}
			if !end.IsZero() && c.UnixTime >= end.Unix() {
				break
			}

			v := new(internal.Candle)
			if err := json.Unmarshal([]byte(c.Candle), v); err != nil {
				return fmt.Errorf("could not json-unmarshal candle data: %w", err)
			}
			gv := &gobs.Candle{
				StartTime: gobs.RemoteTime{Time: time.Unix(c.UnixTime, 0)},
				Duration:  time.Minute,
				Low:       v.Low.Decimal,
				High:      v.High.Decimal,
				Open:      v.Open.Decimal,
				Close:     v.Close.Decimal,
				Volume:    v.Volume.Decimal,
			}

			if err := fn(gv); err != nil {
				return err
			}
		}
		return nil
	}
	return kvutil.AscendDB[gobs.CoinbaseCandles](ctx, ds.db, minKey, maxKey, scanner)
}

func (ds *Datastore) maybeSaveOrders(ctx context.Context, orders []*internal.Order) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	var filled []*internal.Order
	for _, order := range orders {
		if !slices.Contains(doneStatuses, order.Status) {
			continue
		}
		if order.FilledSize.Decimal.IsZero() {
			continue
		}
		if _, ok := slices.BinarySearchFunc(ds.recentlySaved, order, compareLastFillTime); ok {
			continue
		}
		filled = append(filled, order)
		slices.SortFunc(filled, compareLastFillTime)
	}

	if err := ds.saveOrdersLocked(ctx, filled); err != nil {
		log.Printf("could not save %d filled orders (will retry): %v", len(filled), err)
		return err
	}
	return nil
}

// saveOrdersLocked saves completed orders with non-zero filled size. Orders
// are saved in one key per hour layout. For example, orders in an hour are
// saved at "{root}/filled/product-id/2023-12-01/24" where date and hour are
// picked from the last fill timestamp.
func (ds *Datastore) saveOrdersLocked(ctx context.Context, orders []*internal.Order) error {
	// Prepare hour-key to orders mapping, while also deduping the orders.
	kmap := make(map[string][]*internal.Order)
	for _, v := range orders {
		if v.FilledSize.Decimal.IsZero() {
			return fmt.Errorf("filled size of an order cannot be zero")
		}

		filledAt := v.LastFillTime.Time.UTC()
		k1 := fmt.Sprintf("%4d-%02d-%02d", filledAt.Year(), filledAt.Month(), filledAt.Day())
		k2 := fmt.Sprintf("%02d", filledAt.Hour())
		key := path.Join(Keyspace, "filled", k1, k2)

		vs := kmap[key]
		kmap[key] = append(vs, v)
		log.Printf("saving order %s with filled size %s and last-fill time %s in key %s", v.OrderID, v.FilledSize.Decimal.StringFixed(3), v.LastFillTime, key)
	}

	saver := func(ctx context.Context, rw kv.ReadWriter) error {
		for key, orders := range kmap {
			value, err := kvutil.Get[gobs.CoinbaseOrders](ctx, rw, key)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("could not load coinbase orders at %q: %w", key, err)
				}
				value = &gobs.CoinbaseOrders{
					OrderMap:           make(map[string]json.RawMessage),
					ProductOrderIDsMap: make(map[string][]string),
				}
			}

			for _, v := range orders {
				if err := ds.saveOrderLocked(ctx, rw, v); err != nil {
					return err
				}

				if value.ProductOrderIDsMap == nil {
					value.ProductOrderIDsMap = make(map[string][]string)
				}
				ids := append(value.ProductOrderIDsMap[v.ProductID], v.OrderID)
				sort.Strings(ids)
				value.ProductOrderIDsMap[v.ProductID] = slices.Compact(ids)

				value.OrderMap = nil // Deprecated field.
			}
			if err := kvutil.Set(ctx, rw, key, value); err != nil {
				return fmt.Errorf("could not update coinbase orders at %q: %w", key, err)
			}
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, ds.db, saver); err != nil {
		return err
	}

	for _, vs := range kmap {
		ds.recentlySaved = append(ds.recentlySaved, vs...)
	}
	slices.SortFunc(ds.recentlySaved, compareLastFillTime)
	slices.CompactFunc(ds.recentlySaved, equalLastFillTime)
	return nil
}

func (ds *Datastore) saveOrderLocked(ctx context.Context, rw kv.ReadWriter, v *internal.Order) error {
	key := path.Join(Keyspace, "orders", v.OrderID)
	js, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("could not json-marshal coinbase order: %w", err)
	}
	value := &gobs.CoinbaseOrder{
		OrderID: v.OrderID,
		Order:   json.RawMessage(js),
	}
	if err := kvutil.Set(ctx, rw, key, value); err != nil {
		return fmt.Errorf("could not save coinbase order at key %q: %w", key, err)
	}
	return nil
}

func (ds *Datastore) loadOrderLocked(ctx context.Context, r kv.Reader, orderID string) (*internal.Order, error) {
	key := path.Join(Keyspace, "orders", orderID)
	v, err := kvutil.Get[gobs.CoinbaseOrder](ctx, r, key)
	if err != nil {
		return nil, fmt.Errorf("could not load coinbase order: %w", err)
	}
	order := new(internal.Order)
	if err := json.Unmarshal([]byte(v.Order), order); err != nil {
		return nil, fmt.Errorf("could not json-marshal coinbase order: %w", err)
	}
	return order, nil
}
