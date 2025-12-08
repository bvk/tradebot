// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"path"
	"time"

	"github.com/bvk/tradebot/ctxutil"
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

func (v *Looper) LogDebugInfo() {
	var bought decimal.Decimal
	for _, b := range v.buys {
		bought = bought.Add(b.FilledSize())
	}
	var sold decimal.Decimal
	for _, s := range v.sells {
		sold = sold.Add(s.FilledSize())
	}

	numBuys, pbuy := bought.QuoRem(v.buyPoint.Size, 16)
	numSells, psell := sold.QuoRem(v.sellPoint.Size, 16)
	nbuys, nsells := numBuys.IntPart(), numSells.IntPart()
	holdings := bought.Sub(sold)
	log.Printf("bought=%v sold=%v numBuys=%v pbuy=%v numSells=%v psell=%v, nbuys=%d nsells=%d holdings=%v", bought, sold, numBuys, pbuy, numSells, psell, nbuys, nsells, holdings)
}

func (v *Looper) Run(ctx context.Context, rt *trader.Runtime) error {
	v.runtimeLock.Lock()
	defer v.runtimeLock.Unlock()

	if v.summary.Load() == nil {
		if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
			return err
		}
	}

	jobUpdatesCh := trader.GetJobUpdateChannel(ctx)
	if jobUpdatesCh == nil {
		slog.Warn("jobs updates channel is nil (ignored)", "looper", v)
	}

	for ctx.Err() == nil {
		var bought decimal.Decimal
		for _, b := range v.buys {
			bought = bought.Add(b.FilledSize())
		}
		var sold decimal.Decimal
		for _, s := range v.sells {
			sold = sold.Add(s.FilledSize())
		}

		numBuys, pbuy := bought.QuoRem(v.buyPoint.Size, 16)
		numSells, psell := sold.QuoRem(v.sellPoint.Size, 16)
		nbuys, nsells := numBuys.IntPart(), numSells.IntPart()
		holdings := bought.Sub(sold)

		action := "STOP"
		switch {
		case nbuys < nsells || holdings.IsNegative():
			action = "STOP"

		case nbuys > nsells:
			action = "SELL"

		case pbuy.IsZero() && psell.IsZero():
			action = "BUY"

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

		slog.Info("", "looper", v, "next-action", action, "bought", bought, "sold", sold, "numBuys", numBuys, "pbuy", pbuy, "numSells", numSells, "psell", psell, "nbuys", nbuys, "nsells", nsells, "holdings", holdings)

		switch action {
		default: // STOP
			slog.Error("looper instance is stopped", "looper", v, "bought", bought, "sold", sold, "holdings", holdings, "nbuys", nbuys, "nsells", nsells)
			var bought decimal.Decimal
			for i, b := range v.buys {
				size := b.FilledSize()
				slog.Warn("looper buy history", "looper", v, "buyer", i, "limiter", b, "filled-size", size)
				bought = bought.Add(size)
			}
			var sold decimal.Decimal
			for i, s := range v.sells {
				size := s.FilledSize()
				slog.Warn("looper sell history", "looper", v, "seller", i, "limiter", s, "filled-size", size)
				sold = sold.Add(size)
			}
			<-ctx.Done()
			return context.Cause(ctx)

		case "BUY":
			slog.Debug("starting/resuming a limiter buy", "looper", v, "bought", bought, "sold", sold, "holdings", holdings)

			if len(v.buys) == 0 || v.buys[len(v.buys)-1].PendingSize().IsZero() {
				if err := v.addNewBuy(ctx, rt); err != nil {
					if ctx.Err() == nil {
						slog.Error("could not add a new buyer (will retry)", "looper", v, "nbuys", nbuys, "err", err)
						ctxutil.Sleep(ctx, time.Second)
						continue
					}
					continue
				}
			}

			buyer := v.buys[len(v.buys)-1]
			v.dirtyLimiters.Store(buyer, struct{}{})
			if err := buyer.Run(ctx, rt); err != nil {
				if ctx.Err() == nil {
					slog.Error("could not resume last buyer (will retry)", "looper", v, "nbuys", nbuys, "err", err)
					ctxutil.Sleep(ctx, time.Second)
					continue
				}
				continue
			}

			// Force an update to the job summary.
			v.summary.Store(nil)
			if jobUpdatesCh != nil {
				jobUpdatesCh <- v.UID()
			}

		case "SELL":
			// Start a sell if holding amount is greater than sell size.
			if action == "SELL" {
				slog.Debug("starting/resuming a limiter sell", "looper", v, "bought", bought, "sold", sold, "holdings", holdings)

				if len(v.sells) == 0 || v.sells[len(v.sells)-1].PendingSize().IsZero() {
					if err := v.addNewSell(ctx, rt); err != nil {
						if ctx.Err() == nil {
							slog.Error("could not add new sell limiter (will retry)", "looper", v, "nsells", nsells, "err", err)
							ctxutil.Sleep(ctx, time.Second)
							continue
						}
						continue
					}
				}

				seller := v.sells[len(v.sells)-1]
				v.dirtyLimiters.Store(seller, struct{}{})
				if err := seller.Run(ctx, rt); err != nil {
					if ctx.Err() == nil {
						slog.Error("could not resume last sell limiter (will retry)", "looper", v, "nsells", nsells, "err", err)
						ctxutil.Sleep(ctx, time.Second)
						continue
					}
					continue
				}

				// Force an update to the job summary.
				v.summary.Store(nil)
				if jobUpdatesCh != nil {
					jobUpdatesCh <- v.UID()
				}

				sell, buy := v.sells[len(v.sells)-1], v.buys[len(v.buys)-1]
				fees := sell.Fees().Add(buy.Fees())
				profit := sell.SoldValue().Sub(buy.BoughtValue()).Sub(fees)
				slog.Info("SendMessage", "looper", v, "profit", profit, "nsells", len(v.sells), "nbuys", len(v.buys), "last.sell.fees", sell.Fees(), "last.buy.fees", buy.Fees(), "last.buy.value", buy.BoughtValue())
				rt.Messenger.SendMessage(ctx, time.Now(), "A sell is completed successfully at price %s in product %s (%s) with %s of profit.", v.sellPoint.Price.StringFixed(3), v.productID, v.exchangeName, profit.StringFixed(3))
			}
		}
	}
	return context.Cause(ctx)
}

func (v *Looper) addNewBuy(ctx context.Context, rt *trader.Runtime) error {
	slog.Info("adding new buy limiter", "looper", v, "buyer", fmt.Sprintf("buy-%06d", len(v.buys)), "buy-price", v.buyPoint.Price.StringFixed(3))

	uid := path.Join(v.uid, fmt.Sprintf("buy-%06d", len(v.buys)))
	b, err := limiter.New(uid, v.exchangeName, v.productID, &v.buyPoint)
	if err != nil {
		return err
	}
	v.buys = append(v.buys, b)
	v.dirtyLimiters.Store(b, struct{}{})

	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		v.buys = v.buys[:len(v.buys)-1]
		return err
	}
	return nil
}

func (v *Looper) addNewSell(ctx context.Context, rt *trader.Runtime) error {
	slog.Info("adding new sell limiter", "looper", v, "seller", fmt.Sprintf("sell-%06d", len(v.sells)))

	uid := path.Join(v.uid, fmt.Sprintf("sell-%06d", len(v.sells)))
	s, err := limiter.New(uid, v.exchangeName, v.productID, &v.sellPoint)
	if err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	v.dirtyLimiters.Store(s, struct{}{})

	if err := kv.WithReadWriter(ctx, rt.Database, v.Save); err != nil {
		v.sells = v.sells[:len(v.sells)-1]
		return err
	}
	return nil
}
