// Copyright (c) 2023 BVK Chaitanya

package looper

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/point"
	"github.com/bvkgo/kv"
)

const DefaultKeyspace = "/loopers/"

type Looper struct {
	productID    string
	exchangeName string

	uid string

	buyPoint  point.Point
	sellPoint point.Point

	buys  []*limiter.Limiter
	sells []*limiter.Limiter
}

type Status struct {
	UID string

	ProductID string

	BuyPoint  point.Point
	SellPoint point.Point

	NumBuys  int
	NumSells int
}

func New(uid, exchangeName, productID string, buy, sell *point.Point) (*Looper, error) {
	v := &Looper{
		productID: productID,
		uid:       uid,
		buyPoint:  *buy,
		sellPoint: *sell,
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}

func (v *Looper) check() error {
	if len(v.uid) == 0 {
		return fmt.Errorf("looper uid is empty")
	}
	if err := v.buyPoint.Check(); err != nil {
		return fmt.Errorf("buy point %v is invalid", v.buyPoint)
	}
	if side := v.buyPoint.Side(); side != "BUY" {
		return fmt.Errorf("buy point %v has invalid side", v.buyPoint)
	}
	if err := v.sellPoint.Check(); err != nil {
		return fmt.Errorf("sell point %v is invalid", v.sellPoint)
	}
	if side := v.sellPoint.Side(); side != "SELL" {
		return fmt.Errorf("sell point %v has invalid side", v.sellPoint)
	}
	return nil
}

func (v *Looper) String() string {
	return "looper:" + v.uid
}

func (v *Looper) UID() string {
	return v.uid
}

func (v *Looper) ProductID() string {
	return v.productID
}

func (v *Looper) ExchangeName() string {
	return v.exchangeName
}

func (v *Looper) Status() *Status {
	return &Status{
		UID:       v.uid,
		ProductID: v.productID,
		BuyPoint:  v.buyPoint,
		SellPoint: v.sellPoint,
		NumBuys:   len(v.buys), // FIXME: Remove the incomplete ones?
		NumSells:  len(v.sells),
	}
}

func (v *Looper) Fix(ctx context.Context, product exchange.Product, db kv.Database) error {
	for _, b := range v.buys {
		if err := b.Fix(ctx, product, db); err != nil {
			return err
		}
	}
	for _, s := range v.sells {
		if err := s.Fix(ctx, product, db); err != nil {
			return err
		}
	}
	return nil
}

func (v *Looper) Run(ctx context.Context, product exchange.Product, db kv.Database) error {
	for ctx.Err() == nil {
		nbuys, nsells := len(v.buys), len(v.sells)

		if nbuys == 0 {
			if err := v.addNewBuy(ctx, product, db); err != nil {
				if ctx.Err() == nil {
					log.Printf("could not add limit-buy %d (retrying): %v", nbuys, err)
					time.Sleep(time.Second)
				}
			}
			continue
		}

		if last := v.buys[nbuys-1]; !last.Pending().IsZero() {
			if err := last.Run(ctx, product, db); err != nil {
				if ctx.Err() == nil {
					log.Printf("limit-buy %d has failed (retrying): %v", nbuys, err)
					time.Sleep(time.Second)
				}
			}
			continue
		}

		if nsells < nbuys {
			if err := v.addNewSell(ctx, product, db); err != nil {
				if ctx.Err() == nil {
					log.Printf("could not add limit-sell %d (retrying); %v", nsells, err)
					time.Sleep(time.Second)
				}
			}
			continue
		}

		if last := v.sells[nsells-1]; !last.Pending().IsZero() {
			if err := last.Run(ctx, product, db); err != nil {
				if ctx.Err() == nil {
					log.Printf("limit-sell %d has failed (retrying): %v", nsells, err)
					time.Sleep(time.Second)
				}
			}
			continue
		}

		if err := v.addNewBuy(ctx, product, db); err != nil {
			if ctx.Err() == nil {
				log.Printf("could not add limit-buy %d (retrying): %v", nbuys, err)
				time.Sleep(time.Second)
			}
			continue
		}
	}

	return context.Cause(ctx)
}

func (v *Looper) addNewBuy(ctx context.Context, product exchange.Product, db kv.Database) error {
	// Wait for the ticker to go above the buy point price.
	tickerCh := product.TickerCh()
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
	if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
		v.buys = v.buys[:len(v.buys)-1]
		return err
	}
	return nil
}

func (v *Looper) addNewSell(ctx context.Context, product exchange.Product, db kv.Database) error {
	// // Wait for the ticker to go below the sell point price.
	// tickerCh := product.TickerCh()
	// for p := v.sellPoint.Price; p.GreaterThanOrEqual(v.sellPoint.Price); {
	// 	log.Printf("%v:%v:%v waiting for the ticker price to go below sell point", v.uid, v.buyPoint, v.sellPoint)
	// 	select {
	// 	case <-ctx.Done():
	// 		return context.Cause(ctx)
	// 	case ticker := <-tickerCh:
	// 		p = ticker.Price
	// 	}
	// }

	uid := path.Join(v.uid, fmt.Sprintf("sell-%06d", len(v.sells)))
	s, err := limiter.New(uid, v.exchangeName, v.productID, &v.sellPoint)
	if err != nil {
		return err
	}
	v.sells = append(v.sells, s)
	if err := kv.WithReadWriter(ctx, db, v.Save); err != nil {
		v.sells = v.sells[:len(v.sells)-1]
		return err
	}
	return nil
}

func (v *Looper) Save(ctx context.Context, rw kv.ReadWriter) error {
	var limiters []string
	for _, b := range v.buys {
		if err := b.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save child limiter: %w", err)
		}
		s := b.Status()
		limiters = append(limiters, s.UID)
	}
	for _, s := range v.sells {
		if err := s.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save child limiter: %w", err)
		}
		ss := s.Status()
		limiters = append(limiters, ss.UID)
	}
	gv := &gobs.LooperState{
		V2: &gobs.LooperStateV2{
			ProductID:    v.productID,
			ExchangeName: v.exchangeName,
			LimiterIDs:   limiters,
			TradePair: gobs.Pair{
				Buy: gobs.Point{
					Size:   v.buyPoint.Size,
					Price:  v.buyPoint.Price,
					Cancel: v.buyPoint.Cancel,
				},
				Sell: gobs.Point{
					Size:   v.sellPoint.Size,
					Price:  v.sellPoint.Price,
					Cancel: v.sellPoint.Cancel,
				},
			},
		},
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(gv); err != nil {
		return fmt.Errorf("could not encode looper state: %w", err)
	}
	key := v.uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(v.uid, "/wallers")
		key = path.Join(DefaultKeyspace, v)
	}
	if err := rw.Set(ctx, key, &buf); err != nil {
		return fmt.Errorf("could not save looper state: %w", err)
	}
	return nil
}

func Load(ctx context.Context, uid string, r kv.Reader) (*Looper, error) {
	key := uid
	if !strings.HasPrefix(key, DefaultKeyspace) {
		v := strings.TrimPrefix(uid, "/wallers")
		key = path.Join(DefaultKeyspace, v) // TODO: Make this default
	}
	gv, err := kvutil.Get[gobs.LooperState](ctx, r, key)
	if errors.Is(err, os.ErrNotExist) {
		gv, err = kvutil.Get[gobs.LooperState](ctx, r, uid) // TODO: Remove after prod rollout
	}
	if err != nil {
		return nil, err
	}
	gv.Upgrade()
	var buys, sells []*limiter.Limiter
	for _, id := range gv.V2.LimiterIDs {
		v, err := limiter.Load(ctx, id, r)
		if err != nil {
			return nil, err
		}
		if v.Side() == "BUY" {
			buys = append(buys, v)
			continue
		}
		if v.Side() == "SELL" {
			sells = append(sells, v)
			continue
		}
		return nil, fmt.Errorf("unexpected limiter side %q", v.Side())
	}

	v := &Looper{
		uid:          uid,
		productID:    gv.V2.ProductID,
		exchangeName: gv.V2.ExchangeName,
		buys:         buys,
		sells:        sells,
		buyPoint: point.Point{
			Size:   gv.V2.TradePair.Buy.Size,
			Price:  gv.V2.TradePair.Buy.Price,
			Cancel: gv.V2.TradePair.Buy.Cancel,
		},
		sellPoint: point.Point{
			Size:   gv.V2.TradePair.Sell.Size,
			Price:  gv.V2.TradePair.Sell.Price,
			Cancel: gv.V2.TradePair.Sell.Cancel,
		},
	}
	if err := v.check(); err != nil {
		return nil, err
	}
	return v, nil
}
