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

func (ex *Exchange) goSaveOrders(ctx context.Context) {
	r, ch, err := ex.rawOrderTopic.Subscribe(0, true /* includeRecent */)
	if err != nil {
		return
	}
	defer r.Unsubscribe()

	var timerCh <-chan time.Time
	var filled []*internal.Order
	localCtx := context.Background()

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			if len(filled) > 0 {
				if err := ex.datastore.saveOrdersHourly(localCtx, filled); err != nil {
					log.Printf("could not save order (ignored): %v", err)
				}
				log.Printf("saved %d orders (due to close) from coinbase with min fillled timestamp %s", len(filled), filled[0].LastFillTime.Time)
			}
			continue

		case order := <-ch:
			if !slices.Contains(doneStatuses, order.Status) {
				continue
			}
			if order.FilledSize.Decimal.IsZero() {
				continue
			}
			if _, ok := slices.BinarySearchFunc(filled, order, compareInternalOrder); ok {
				continue
			}
			filled = append(filled, order)
			slices.SortFunc(filled, compareInternalOrder)

			// Do not save orders filled in the current-hour. This will reduce the
			// load on the database.
			currentHour := time.Now().Truncate(time.Hour)
			minFilled := slices.MinFunc(filled, compareLastFillTime)
			if minFilled.LastFillTime.Time.After(currentHour) {
				log.Printf("added order %s with size %s and fill-time %s to the batch", order.OrderID, order.FilledSize.Decimal, order.LastFillTime.Time)
				continue
			}

			// Batch as many as possible and also start a timer.
			maxFilled := slices.MaxFunc(filled, compareLastFillTime)
			if n := len(filled); n < 100 {
				log.Printf("collected %d orders with fill times between %s - %s", len(filled), minFilled.LastFillTime, maxFilled.LastFillTime)
				if timerCh == nil {
					timerCh = time.After(time.Minute)
				}
				continue
			}

			log.Printf("saving %d orders from coinbase with fillled timestamps between %s - %s", len(filled), minFilled.LastFillTime, maxFilled.LastFillTime)
			if err := ex.datastore.saveOrdersHourly(localCtx, filled); err != nil {
				log.Printf("could not save order (will retry): %v", err)
				continue
			}
			log.Printf("saved %d orders from coinbase with min fillled timestamp %s", len(filled), minFilled.LastFillTime.Time)
			filled = filled[:0]
			timerCh = nil

		case <-timerCh:
			timerCh = nil
			minFilled := slices.MinFunc(filled, compareLastFillTime)
			maxFilled := slices.MaxFunc(filled, compareLastFillTime)
			log.Printf("saving %d orders after timeout with fillled timestamps between %s - %s", len(filled), minFilled.LastFillTime, maxFilled.LastFillTime)
			if err := ex.datastore.saveOrdersHourly(localCtx, filled); err != nil {
				log.Printf("could not save order (will retry): %v", err)
				timerCh = time.After(time.Minute)
				continue
			}
			log.Printf("saved %d orders (due to timeout) from coinbase with min fillled timestamp %s", len(filled), minFilled.LastFillTime.Time)
			filled = filled[:0]
		}
	}
	return
}

// saveOrdersHourly saves completed orders with non-zero filled size. Orders
// are saved in one key per hour layout. For example, orders in an hour are
// saved at "{root}/filled/product-id/2023-12-01/24" where date and hour are
// picked from the last fill timestamp.
func (ds *Datastore) saveOrdersHourly(ctx context.Context, orders []*internal.Order) error {
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

	return nil
}
