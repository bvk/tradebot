// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"slices"
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

	deferredToSave []*internal.Order
	recentlySaved  []*internal.Order
}

func NewDatastore(db kv.Database) *Datastore {
	return &Datastore{
		db: db,
	}
}

func (ds *Datastore) ScanFilled(ctx context.Context, product string, fn func(string, *exchange.Order) error) error {
	begin, end := kvutil.PathRange(path.Join(Keyspace, "filled"))
	wrapper := func(ctx context.Context, r kv.Reader, key string, value *gobs.CoinbaseOrders) error {
		for id, str := range value.OrderMap {
			order := new(internal.Order)
			if err := json.NewDecoder(bytes.NewReader([]byte(str))).Decode(order); err != nil {
				return fmt.Errorf("could not json-decode order %s at key %q: %w", id, key, err)
			}
			if len(product) > 0 && order.ProductID != product {
				continue
			}
			if err := fn(order.ProductID, exchangeOrderFromOrder(order)); err != nil {
				return err
			}
		}
		return nil
	}
	return kvutil.AscendDB(ctx, ds.db, begin, end, wrapper)
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

func (ds *Datastore) maybeSaveOrders(ctx context.Context, orders []*internal.Order) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	lastSize := len(ds.deferredToSave)
	for _, order := range orders {
		if !slices.Contains(doneStatuses, order.Status) {
			continue
		}
		if order.FilledSize.Decimal.IsZero() {
			continue
		}
		if _, ok := slices.BinarySearchFunc(ds.deferredToSave, order, compareLastFillTime); ok {
			continue
		}
		if _, ok := slices.BinarySearchFunc(ds.recentlySaved, order, compareLastFillTime); ok {
			continue
		}
		ds.deferredToSave = append(ds.deferredToSave, order)
		slices.SortFunc(ds.deferredToSave, compareLastFillTime)
	}

	if len(ds.deferredToSave) == lastSize {
		return
	}

	minFilled := slices.MinFunc(ds.deferredToSave, compareLastFillTime)
	readyHour := time.Now().Add(-time.Hour).Truncate(time.Hour)
	if minFilled.LastFillTime.Time.After(readyHour) {
		log.Printf("deferring %d orders cause min fill-time %s is within current hour %s", len(ds.deferredToSave), minFilled.LastFillTime, readyHour)
		return
	}

	var pending []*internal.Order
	var beforeReadyHour []*internal.Order
	for i, order := range ds.deferredToSave {
		if order.LastFillTime.Time.Before(readyHour) {
			beforeReadyHour = append(beforeReadyHour, order)
			continue
		}
		pending = slices.Clone(ds.deferredToSave[i+1:])
		break
	}

	if len(beforeReadyHour) == 0 {
		return
	}

	log.Printf("saving %d orders and deferring %d orders cause former are filled before ready time %s", len(beforeReadyHour), len(pending), readyHour)
	if err := ds.saveOrdersLocked(ctx, beforeReadyHour); err != nil {
		log.Printf("could not save %d orders filled before current hour %s (will retry): %v", len(beforeReadyHour), readyHour, err)
		return
	}
	ds.deferredToSave = pending
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

	for key, orders := range kmap {
		value, err := kvutil.GetDB[gobs.CoinbaseOrders](ctx, ds.db, key)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("could not load coinbase orders at %q: %w", key, err)
			}
			value = &gobs.CoinbaseOrders{
				OrderMap: make(map[string]json.RawMessage),
			}
		}
		for _, v := range orders {
			if _, ok := value.OrderMap[v.OrderID]; !ok {
				js, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("could not json-marshal order: %w", err)
				}
				if value.OrderMap == nil {
					value.OrderMap = make(map[string]json.RawMessage)
				}
				value.OrderMap[v.OrderID] = json.RawMessage(js)
				continue
			}
		}
		value.RawOrderMap = nil
		if err := kvutil.SetDB(ctx, ds.db, key, value); err != nil {
			return fmt.Errorf("could not update coinbase orders at %q: %w", key, err)
		}
	}

	for _, vs := range kmap {
		ds.recentlySaved = append(ds.recentlySaved, vs...)
	}
	slices.SortFunc(ds.recentlySaved, compareLastFillTime)
	slices.CompactFunc(ds.recentlySaved, equalLastFillTime)
	// TODO: Drop very old orders.
	return nil
}
