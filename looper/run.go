// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"fmt"
	"log"
	"path"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/shopspring/decimal"
)

func (v *Looper) Fix(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

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
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

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

func (v *Looper) readyWaitForBuy(ctx context.Context, rt *trader.Runtime) {
	tickerCh, stopTickers := rt.Product.TickerCh()
	defer stopTickers()

	var tick *exchange.Ticker
	for !v.isReady(tick) {
		select {
		case <-ctx.Done():
			return
		case tick = <-tickerCh:
		}
	}
}

func (v *Looper) readyWaitForSell(ctx context.Context, rt *trader.Runtime) {
	tickerCh, stopTickers := rt.Product.TickerCh()
	defer stopTickers()

	var tick *exchange.Ticker
	for !v.isReady(tick) {
		select {
		case <-ctx.Done():
			return
		case tick = <-tickerCh:
			if tick.Price.GreaterThanOrEqual(v.sellPoint.Price) {
				return
			}
		}
	}
}

func (v *Looper) Run(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	for ctx.Err() == nil {
		var bought decimal.Decimal
		for _, b := range v.buys {
			bought = bought.Add(b.FilledSize())
		}
		var sold decimal.Decimal
		for _, s := range v.sells {
			sold = sold.Add(s.FilledSize())
		}

		numBuys, pbuy := bought.QuoRem(v.buyPoint.Size, 6)
		numSells, psell := sold.QuoRem(v.sellPoint.Size, 6)
		nbuys, nsells := numBuys.IntPart(), numSells.IntPart()
		holdings := bought.Sub(sold)

		action := "STOP"
		switch {
		case nbuys < nsells || holdings.IsNegative():
			action = "STOP"

		case pbuy.IsZero() && psell.IsZero():
			// When no partial buys/sells are present, next action depends on number
			// of sells vs number of buys.
			if nbuys == nsells {
				action = "BUY"
			} else if nbuys > nsells {
				action = "SELL"
			} else { // nbuys < nsells is unexpected
				action = "STOP"
			}

		case !pbuy.IsZero() && !psell.IsZero():
			// When buys and sells are both partial, then we have a bug, we must stop
			// this job completely.
			action = "STOP"

		case pbuy.IsZero() && !psell.IsZero():
			// When buy is full, but sell is partial, we should complete the sell.
			action = "SELL"

		case !pbuy.IsZero() && psell.IsZero():
			// When sell is full, but buy is partial, we should complete the buy.
			action = "BUY"
		}

		switch action {
		default: // STOP
			log.Printf("%s: STOPPED: bought (%s) and sold (%s) sizes with holding=%s are inconsistent (out of %d buys and %d sells)", v.uid, bought, sold, holdings, nbuys, nsells)
			var bought decimal.Decimal
			for i, b := range v.buys {
				size := b.FilledSize()
				log.Printf("%s: WARNING: buyer %d (%s) has filled size %s", v.uid, i, b.UID(), size)
				bought = bought.Add(size)
			}
			var sold decimal.Decimal
			for i, s := range v.sells {
				size := s.FilledSize()
				log.Printf("%s: WARNING: seller %d (%s) has filled size %s", v.uid, i, s.UID(), size)
				sold = sold.Add(size)
			}
			<-ctx.Done()
			return context.Cause(ctx)

		case "BUY":
			v.readyWaitForBuy(ctx, rt)
			log.Printf("%s: current holding size is %s-%s=%s (starting a buy)", v.uid, bought, sold, holdings)

			if len(v.buys) == 0 || v.buys[len(v.buys)-1].PendingSize().IsZero() {
				if err := v.addNewBuy(ctx, rt); err != nil {
					if ctx.Err() == nil {
						log.Printf("could not add limit-buy %d (retrying): %v", nbuys, err)
						time.Sleep(time.Second)
						continue
					}
					log.Printf("%v: could not create new limit-buy op (will retry): %v", v.uid, err)
					continue
				}
			}

			if err := v.buys[len(v.buys)-1].Run(ctx, rt); err != nil {
				if ctx.Err() == nil {
					log.Printf("limit-buy %d has failed (retrying): %v", nbuys, err)
					time.Sleep(time.Second)
					continue
				}
				log.Printf("%v: could not complete limit-buy op (will retry): %v", v.uid, err)
				continue
			}

		case "SELL":
			// Start a sell if holding amount is greater than sell size.
			if action == "SELL" {
				v.readyWaitForSell(ctx, rt)
				log.Printf("%s: current holding size is %s-%s=%s (starting a sell)", v.uid, bought, sold, holdings)

				if len(v.sells) == 0 || v.sells[len(v.sells)-1].PendingSize().IsZero() {
					if err := v.addNewSell(ctx, rt); err != nil {
						if ctx.Err() == nil {
							log.Printf("could not add limit-sell %d (retrying); %v", nsells, err)
							time.Sleep(time.Second)
							continue
						}
						log.Printf("%v: could not create new limit-sell op (will retry): %v", v.uid, err)
						continue
					}
				}

				if err := v.sells[len(v.sells)-1].Run(ctx, rt); err != nil {
					if ctx.Err() == nil {
						log.Printf("limit-sell %d has failed (retrying): %v", nsells, err)
						time.Sleep(time.Second)
						continue
					}
					log.Printf("%v: could not complete limit-sell op (will retry): %v", v.uid, err)
					continue
				}

				sell, buy := v.sells[len(v.sells)-1], v.buys[len(v.buys)-1]
				fees := sell.Fees().Add(buy.Fees())
				profit := sell.SoldValue().Sub(buy.BoughtValue()).Sub(fees)
				rt.Messenger.SendMessage(ctx, time.Now(), "A sell is completed successfully at price %s in product %s (%s) with %s of profit.", v.sellPoint.Price.StringFixed(3), v.productID, v.exchangeName, profit.StringFixed(3))
			}
		}
	}
	return context.Cause(ctx)
}

func (v *Looper) addNewBuy(ctx context.Context, rt *trader.Runtime) error {
	// Wait for the ticker to go above the buy point price.
	tickerCh, stopTickers := rt.Product.TickerCh()
	defer stopTickers()

	var curPrice decimal.Decimal
	for curPrice.IsZero() || curPrice.LessThanOrEqual(v.buyPoint.Price) {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case ticker := <-tickerCh:
			curPrice = ticker.Price
		}
	}
	log.Printf("%s: adding new limit-buy buy-%06d at buy-price %s when current price is %s", v.uid, len(v.buys), v.buyPoint.Price.StringFixed(3), curPrice.StringFixed(3))

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
