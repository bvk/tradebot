// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/coinbase/internal"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
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

	wrapper := func(ctx context.Context, r kv.Reader, k string, v *gobs.CoinbaseOrderIDs) error {
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

func (ds *Datastore) lastCandlesTime(ctx context.Context) (time.Time, error) {
	minKey := path.Join(Keyspace, "candles", "0000-00-00/00")
	maxKey := path.Join(Keyspace, "candles", "9999-99-99/99")
	key, _, err := kvutil.LastDB[gobs.CoinbaseCandles](ctx, ds.db, minKey, maxKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return time.Time{}, fmt.Errorf("could not fetch last candles key: %w", err)
		}
	}
	if len(key) == 0 {
		return time.Date(2023, 9, 24, 0, 0, 0, 0, time.UTC), nil
	}
	str := strings.TrimPrefix(key, path.Join(Keyspace, "candles"))
	var y, m, d, h int
	if _, err := fmt.Sscanf(str, "/%04d-%02d-%02d/%02d", &y, &m, &d, &h); err != nil {
		return time.Time{}, fmt.Errorf("could not scan timestamp fields from %q: %w", str, err)
	}
	return time.Date(y, time.Month(m), d, h, 0, 0, 0, time.UTC), nil
}

func (ds *Datastore) LastFilledTime(ctx context.Context) (time.Time, error) {
	minKey := path.Join(Keyspace, "filled", "0000-00-00/00")
	maxKey := path.Join(Keyspace, "filled", "9999-99-99/99")
	key, _, err := kvutil.LastDB[gobs.CoinbaseOrderIDs](ctx, ds.db, minKey, maxKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return time.Time{}, fmt.Errorf("could not fetch last filled key: %w", err)
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
		key := path.Join(Keyspace, "filled", filledAt.Format("2006-01-02/15"))

		vs := kmap[key]
		kmap[key] = append(vs, v)
		log.Printf("saving %s order %s in %s with filled size %s, price %s and last-fill time %s in key %s", v.Side, v.OrderID, v.ProductID, v.FilledSize.Decimal, v.AvgFilledPrice.Decimal, v.LastFillTime, key)
	}

	saver := func(ctx context.Context, rw kv.ReadWriter) error {
		for key, orders := range kmap {
			value, err := kvutil.Get[gobs.CoinbaseOrderIDs](ctx, rw, key)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("could not load coinbase orders at %q: %w", key, err)
				}
				value = &gobs.CoinbaseOrderIDs{
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

func (ds *Datastore) GetOrder(ctx context.Context, orderID string) (*internal.Order, error) {
	var order *internal.Order
	load := func(ctx context.Context, r kv.Reader) error {
		v, err := ds.loadOrderLocked(ctx, r, orderID)
		if err != nil {
			return err
		}
		order = v
		return nil
	}
	if err := kv.WithReader(ctx, ds.db, load); err != nil {
		return nil, err
	}
	return order, nil
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

func (ds *Datastore) saveProducts(ctx context.Context, ps []*internal.Product) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	return kv.WithReadWriter(ctx, ds.db, func(ctx context.Context, rw kv.ReadWriter) error {
		return ds.saveProductsLocked(ctx, rw, ps)
	})
}

func (ds *Datastore) saveProductsLocked(ctx context.Context, rw kv.ReadWriter, ps []*internal.Product) error {
	sort.Slice(ps, func(i, j int) bool {
		return ps[i].ProductID < ps[j].ProductID
	})

	key := path.Join(Keyspace, "products")
	value := &gobs.CoinbaseProducts{
		Timestamp: time.Now(),
	}
	for _, p := range ps {
		js, err := json.Marshal(p)
		if err != nil {
			return fmt.Errorf("could not json-marshal coinbase product: %w", err)
		}
		value.Products = append(value.Products, &gobs.CoinbaseProduct{
			ProductID: p.ProductID,
			Price:     p.Price.Decimal,
			Product:   json.RawMessage(js),
		})
	}
	if err := kvutil.Set(ctx, rw, key, value); err != nil {
		return fmt.Errorf("could not update products data at key %q: %w", key, err)
	}
	return nil
}

func (ds *Datastore) ProductsPriceMap(ctx context.Context) (map[string]decimal.Decimal, error) {
	pmap := make(map[string]decimal.Decimal)

	collector := func(ctx context.Context, r kv.Reader) error {
		key := path.Join(Keyspace, "products")
		value, err := kvutil.Get[gobs.CoinbaseProducts](ctx, r, key)
		if err != nil {
			return fmt.Errorf("could not load coinbase products information: %w", err)
		}
		for _, p := range value.Products {
			pmap[p.ProductID] = p.Price
		}
		return nil
	}

	if err := kv.WithReader(ctx, ds.db, collector); err != nil {
		return nil, err
	}
	return pmap, nil
}

func (ds *Datastore) saveAccounts(ctx context.Context, as []*internal.Account) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	return kv.WithReadWriter(ctx, ds.db, func(ctx context.Context, rw kv.ReadWriter) error {
		return ds.saveAccountsLocked(ctx, rw, as)
	})
}

func (ds *Datastore) saveAccountsLocked(ctx context.Context, rw kv.ReadWriter, as []*internal.Account) error {
	sort.Slice(as, func(i, j int) bool {
		return as[i].Currency < as[j].Currency
	})

	key := path.Join(Keyspace, "accounts")
	value := &gobs.CoinbaseAccounts{
		Timestamp: time.Now(),
	}
	for _, a := range as {
		js, err := json.Marshal(a)
		if err != nil {
			return fmt.Errorf("could not json-marshal coinbase accounts: %w", err)
		}
		value.Accounts = append(value.Accounts, &gobs.CoinbaseAccount{
			CurrencyID: a.Currency,
			Account:    json.RawMessage(js),
		})
	}
	if err := kvutil.Set(ctx, rw, key, value); err != nil {
		return fmt.Errorf("could not update accounts data at key %q: %w", key, err)
	}
	return nil
}

func (ds *Datastore) LoadAccounts(ctx context.Context) ([]*gobs.Account, error) {
	key := path.Join(Keyspace, "accounts")
	value, err := kvutil.GetDB[gobs.CoinbaseAccounts](ctx, ds.db, key)
	if err != nil {
		return nil, fmt.Errorf("could not load coinbase accounts data: %w", err)
	}
	var accounts []*gobs.Account
	for _, a := range value.Accounts {
		v := new(internal.Account)
		if err := json.Unmarshal([]byte(a.Account), v); err != nil {
			return nil, fmt.Errorf("could not json-unmarshal account data: %w", err)
		}
		if v.AvailableBalance.Value.Decimal.IsZero() && v.Hold.Value.Decimal.IsZero() {
			continue
		}
		ga := &gobs.Account{
			Timestamp:  value.Timestamp,
			Name:       v.Name,
			CurrencyID: a.CurrencyID,
			Available:  v.AvailableBalance.Value.Decimal,
			Hold:       v.Hold.Value.Decimal,
		}
		accounts = append(accounts, ga)
	}
	return accounts, nil
}

func (ds *Datastore) PriceMapAt(ctx context.Context, at time.Time) (pmap map[string]decimal.Decimal, err error) {
	kv.WithReader(ctx, ds.db, func(ctx context.Context, r kv.Reader) error {
		pmap, err = ds.pricesAtLocked(ctx, r, at)
		return nil
	})
	return pmap, err
}

func (ds *Datastore) pricesAtLocked(ctx context.Context, r kv.Reader, at time.Time) (map[string]decimal.Decimal, error) {
	endAt := at.Add(time.Minute).Truncate(time.Minute)
	begin := path.Join(Keyspace, "candles", "0000-00-00/00")
	end := path.Join(Keyspace, "candles", endAt.Format("2006-01-02/15"))
	key, value, err := kvutil.Last[gobs.CoinbaseCandles](ctx, r, begin, end)
	if err != nil {
		return nil, err
	}
	if value == nil || len(value.ProductCandlesMap) == 0 {
		return nil, fmt.Errorf("could not find any candles data before key %q: %w", end, os.ErrNotExist)
	}
	unixAt := at.Unix()
	pmap := make(map[string]decimal.Decimal)
	for p, cs := range value.ProductCandlesMap {
		if len(cs) == 0 {
			continue
		}
		rawCandle := cs[0].Candle
		for _, c := range cs {
			if unixAt < c.UnixTime {
				rawCandle = c.Candle
				break
			}
		}
		candle := new(internal.Candle)
		if err := json.Unmarshal([]byte(rawCandle), candle); err != nil {
			return nil, fmt.Errorf("could not json-unmarshal %q candle at key %q: %w", p, key, err)
		}
		pmap[p] = candle.Close.Decimal
	}
	return pmap, nil
}
