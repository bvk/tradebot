// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
)

func (v *Looper) Fix(ctx context.Context, rt *trader.Runtime) error {
	for _, b := range v.buys {
		if err := b.Fix(ctx, rt); err != nil {
			return err
		}
	}
	for _, s := range v.sells {
		if err := s.Fix(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}

func (v *Looper) Refresh(ctx context.Context, rt *trader.Runtime) error {
	for _, b := range v.buys {
		if err := b.Refresh(ctx, rt); err != nil {
			return err
		}
	}
	for _, s := range v.sells {
		if err := s.Refresh(ctx, rt); err != nil {
			return err
		}
	}
	return nil
}

func (v *Looper) Run(ctx context.Context, rt *trader.Runtime) error {
	for ctx.Err() == nil {
		nbuys, nsells := len(v.buys), len(v.sells)
		if nbuys > 0 && nsells > 0 {
			log.Printf("%s: nbuys %d nsells %d last-buy-pending: %s last-sell-pending: %s", v.uid, nbuys, nsells, v.buys[nbuys-1].PendingSize(), v.sells[nsells-1].PendingSize())
		}

		if nbuys == 0 {
			if err := v.addNewBuy(ctx, rt); err != nil {
				if ctx.Err() == nil {
					log.Printf("could not add limit-buy %d (retrying): %v", nbuys, err)
					time.Sleep(time.Second)
					continue
				}
				log.Printf("%v: could not create new limit-buy op: %v", v.uid, err)
			}
			continue
		}

		if last := v.buys[nbuys-1]; !last.PendingSize().IsZero() {
			if err := last.Run(ctx, rt); err != nil {
				if ctx.Err() == nil {
					log.Printf("limit-buy %d has failed (retrying): %v", nbuys, err)
					time.Sleep(time.Second)
					continue
				}
				log.Printf("%v: could not run limit-buy op: %v", v.uid, err)
			}
			continue
		}

		if nsells < nbuys {
			if err := v.addNewSell(ctx, rt); err != nil {
				if ctx.Err() == nil {
					log.Printf("could not add limit-sell %d (retrying); %v", nsells, err)
					time.Sleep(time.Second)
					continue
				}
				log.Printf("%v: could not create new limit-sell op: %v", v.uid, err)
			}
			continue
		}

		if last := v.sells[nsells-1]; !last.PendingSize().IsZero() {
			if err := last.Run(ctx, rt); err != nil {
				if ctx.Err() == nil {
					log.Printf("limit-sell %d has failed (retrying): %v", nsells, err)
					time.Sleep(time.Second)
					continue
				}
				log.Printf("%v: could not complete limit-sell op: %v", v.uid, err)
			}
			continue
		}

		if err := v.addNewBuy(ctx, rt); err != nil {
			if ctx.Err() == nil {
				log.Printf("could not add limit-buy %d (retrying): %v", nbuys, err)
				time.Sleep(time.Second)
				continue
			}
			log.Printf("%v: could not create new limit-buy op: %v", v.uid, err)
			continue
		}
	}

	return context.Cause(ctx)
}

func (v *Looper) addNewBuy(ctx context.Context, rt *trader.Runtime) error {
	log.Printf("%s: adding new limit-buy buy-%06d", v.uid, len(v.buys))

	// Wait for the ticker to go above the buy point price.
	tctx, tcancel := context.WithCancel(ctx)
	defer tcancel()
	tickerCh := rt.Product.TickerCh(tctx)

	for p := v.buyPoint.Price; p.LessThanOrEqual(v.buyPoint.Price); {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case ticker := <-tickerCh:
			p = ticker.Price
		}
	}

	uid := path.Join(v.uid, fmt.Sprintf("buy-%06d", len(v.buys)))
	b, err := limiter.New(uid, v.exchangeName, v.productID, &v.buyPoint)
	if err != nil {
		return err
	}

	v.buys = append(v.buys, b)
	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		v.buys = v.buys[:len(v.buys)-1]
		return err
	}
	return nil
}

func (v *Looper) addNewSell(ctx context.Context, rt *trader.Runtime) error {
	log.Printf("%s: adding new limit-sell sell-%06d", v.uid, len(v.sells))

	uid := path.Join(v.uid, fmt.Sprintf("sell-%06d", len(v.sells)))
	s, err := limiter.New(uid, v.exchangeName, v.productID, &v.sellPoint)
	if err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		v.sells = v.sells[:len(v.sells)-1]
		return err
	}
	return nil
}
