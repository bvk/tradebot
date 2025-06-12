// Copyright (c) 2024 BVK Chaitanya

package limiter

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvkgo/kv"
)

var activeLimiters syncmap.Map[*Limiter, bool]

// fixFinishTimes is a background job that updates FinishTime field in order
// metadata stored by active and completed limiters.
func fixFinishTimes(ctx context.Context, db kv.Database, ex exchange.Exchange) {
	// Check if a table scan is required.
	fixed := true
	check := func(ctx context.Context, r kv.Reader, key string, value *gobs.LimiterState) error {
		for _, order := range value.V2.ServerIDOrderMap {
			if order.FilledSize.IsZero() {
				continue
			}
			if order.Done == false {
				continue
			}
			if order.FinishTime.Time.IsZero() {
				fixed = false
				break
			}
		}
		return nil
	}
	begin, end := kvutil.PathRange(DefaultKeyspace)
	if err := kvutil.AscendDB(ctx, db, begin, end, check); err != nil && fixed {
		log.Printf("could not scan the limiter state to check for missing finish-times (ignored): %v", err)
	}
	if fixed {
		log.Printf("finish times are already fixed/updated in all limiters")
	}

	next := begin
	fix := func(ctx context.Context, rw kv.ReadWriter) error {
		nextKey, err := fixFinishTime(ctx, rw, ex, next, end, 100)
		if err != nil {
			log.Printf("could not fix finish time in orders (will retry): %v", err)
			return err
		}
		next = nextKey
		return nil
	}

	scanTimeoutCh := time.After(time.Minute)
	activeLimitersCh := time.After(5 * time.Second)

	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			continue

		case <-activeLimitersCh:
			activeLimitersCh = time.After(5 * time.Second)

			activeLimiters.Range(func(l *Limiter, _ bool) bool {
				if err := updateActiveLimiter(ctx, ex, l); err != nil {
					log.Printf("%s: could not update finish time (will retry): %v", l.uid, err)
				} else {
					activeLimiters.Delete(l)
					_ = kv.WithReadWriter(ctx, db, l.Save)
				}
				return true
			})

		case <-scanTimeoutCh:
			if !fixed {
				scanTimeoutCh = time.After(time.Minute)
				if err := kv.WithReadWriter(ctx, db, fix); err != nil {
					log.Printf("could not apply finish time fix (will retry): %v", err)
					continue
				}
				log.Printf("checked/fixed finish-time for limiters between %s-%s", begin, next)
				if next == end {
					fixed = true
					log.Printf("all limiters are scanned and updated with FinishTime fields")
					continue
				}
			}
		}
	}
}

// asyncUpdateFinishTime is invoked by Run method (before it is completed) to
// asynchronously update the FinishTime for orders in the Limiter.
func asyncUpdateFinishTime(v *Limiter) {
	activeLimiters.Store(v, true)
}

func updateActiveLimiter(ctx context.Context, ex exchange.Exchange, v *Limiter) error {
	var status error
	v.orderMap.Range(func(id exchange.OrderID, order *exchange.SimpleOrder) bool {
		if order.FilledSize.IsZero() {
			return true
		}
		if order.Done == false {
			return true
		}
		if !order.FinishTime.Time.IsZero() {
			return true
		}
		v, err := ex.GetOrder(ctx, v.productID, exchange.OrderID(id))
		if err != nil {
			log.Printf("could not fetch order for finish-time (will retry): %v", err)
			status = err
			return false
		}
		order.FinishTime = v.FinishedAt()
		log.Printf("fixed non-existent finish time for just finished order %s to %s", id, v.FinishedAt())
		return true
	})
	return status
}

func updateFinishTime(ctx context.Context, rw kv.ReadWriter, ex exchange.Exchange, key string, value *gobs.LimiterState) error {
	if value == nil {
		v, err := kvutil.Get[gobs.LimiterState](ctx, rw, key)
		if err != nil {
			return err
		}
		value = v
	}

	modified := false
	if value.V2.ExchangeName == "" {
		value.V2.ExchangeName = "coinbase"
		modified = true
	}
	if !strings.EqualFold(value.V2.ExchangeName, ex.ExchangeName()) {
		return nil
	}

	for id, order := range value.V2.ServerIDOrderMap {
		if order.FilledSize.IsZero() {
			continue
		}
		if order.Done == false {
			continue
		}
		if !order.FinishTime.Time.IsZero() {
			continue
		}
		v, err := ex.GetOrder(ctx, value.V2.ProductID, exchange.OrderID(id))
		if err != nil {
			return err
		}
		if v.FinishedAt().IsZero() {
			return fmt.Errorf("finish time is empty for exchange order %s", id)
		}
		order.FinishTime = v.FinishedAt()
		modified = true
		log.Printf("fixed non-existent finish time for order %s to %s", id, v.FinishedAt())
	}

	if modified {
		if err := kvutil.Set(ctx, rw, key, value); err != nil {
			return err
		}
	}
	return nil
}

func fixFinishTime(ctx context.Context, rw kv.ReadWriter, ex exchange.Exchange, begin, end string, max int) (string, error) {
	var ErrSTOP = errors.New("STOP")

	nkeys := 0
	nextKey := end
	upgrade := func(ctx context.Context, _ kv.Reader, key string, value *gobs.LimiterState) error {
		if nkeys == max {
			nextKey = key
			return ErrSTOP
		}

		if err := updateFinishTime(ctx, rw, ex, key, value); err != nil {
			return err
		}

		nkeys++
		return nil
	}

	if err := kvutil.Ascend(ctx, rw, begin, end, upgrade); err != nil {
		if !errors.Is(err, ErrSTOP) {
			return nextKey, err
		}
	}

	return nextKey, nil
}
