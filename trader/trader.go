// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/coinbase"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/google/uuid"
)

type Trader struct {
	closeCtx   context.Context
	closeCause context.CancelCauseFunc

	wg sync.WaitGroup

	db kv.Database

	coinbaseClient *coinbase.Client

	productMap map[string]*coinbase.Product

	handlerMap map[string]http.Handler
}

func NewTrader(secrets *Secrets, db kv.Database) (_ *Trader, status error) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer func() {
		if status != nil {
			cancel(status)
		}
	}()

	var coinbaseClient *coinbase.Client
	if secrets.Coinbase != nil {
		opts := &coinbase.Options{}
		client, err := coinbase.New(secrets.Coinbase.Key, secrets.Coinbase.Secret, opts)
		if err != nil {
			return nil, err
		}
		coinbaseClient = client
	}

	t := &Trader{
		closeCtx:       ctx,
		closeCause:     cancel,
		coinbaseClient: coinbaseClient,
		db:             db,
		handlerMap:     make(map[string]http.Handler),
		productMap:     make(map[string]*coinbase.Product),
	}

	t.handlerMap["/trader/buy/limit"] = httpPostJSONHandler(t.doLimitBuy)
	t.handlerMap["/trader/sell/limit"] = httpPostJSONHandler(t.doLimitSell)
	t.handlerMap["/trader/loop"] = httpPostJSONHandler(t.doLoop)
	t.handlerMap["/trader/limit"] = httpPostJSONHandler(t.doLimit)
	return t, nil
}

func (t *Trader) Close() error {
	t.closeCause(fmt.Errorf("trade is closing: %w", os.ErrClosed))
	t.wg.Wait()

	if t.coinbaseClient != nil {
		for _, p := range t.productMap {
			t.coinbaseClient.CloseProduct(p)
		}
		t.coinbaseClient.Close()
	}
	return nil
}

func (t *Trader) HandlerMap() map[string]http.Handler {
	return maps.Clone(t.handlerMap)
}

func (t *Trader) getProduct(ctx context.Context, name string) (exchange.Product, error) {
	product, ok := t.productMap[name]
	if !ok {
		p, err := t.coinbaseClient.NewProduct(name)
		if err != nil {
			return nil, err
		}
		product = p
		t.productMap[name] = p
	}
	return product, nil
}

func httpPostJSONHandler[T1 any, T2 any](fun func(context.Context, *T1) (*T2, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "invalid http method type", http.StatusMethodNotAllowed)
			return
		}
		if v := r.Header.Get("content-type"); !strings.EqualFold(v, "application/json") {
			http.Error(w, "unsupported content type", http.StatusBadRequest)
			return
		}
		req := new(T1)
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := fun(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsbytes, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(jsbytes)
	})
}

func (t *Trader) doLimit(ctx context.Context, req *LimitRequest) (_ *LimitResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "limit request has failed", "error", status)
		}
	}()

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	point := &Point{
		Size:   req.Size,
		Price:  req.Price,
		Cancel: req.CancelPrice,
	}
	if err := point.Check(); err != nil {
		return nil, err
	}

	uid := path.Join("/limiters", uuid.New().String())
	limit, err := NewLimiter(uid, product, point)
	if err != nil {
		return nil, err
	}

	if err := kv.WithTransaction(ctx, t.db, limit.save); err != nil {
		return nil, err
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := limit.Run(t.closeCtx, t.db); err != nil {
			slog.ErrorContext(t.closeCtx, "limit operation has failed", "error", err)
		} else {
			slog.InfoContext(ctx, "limit has completed successfully")
		}
	}()

	resp := &LimitResponse{
		UID:  uid,
		Side: limit.Side(),
	}
	return resp, nil
}

func (t *Trader) doLimitBuy(ctx context.Context, req *LimitBuyRequest) (_ *LimitBuyResponse, status error) {
	if req.BuyCancelPrice.LessThanOrEqual(req.BuyPrice) {
		return nil, fmt.Errorf("cancel-price must be greater than buy-price")
	}
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "limit-buy has failed", "error", status)
		}
	}()

	// if err := req.Check(); err != nil {
	// 	return err
	// }

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	point := &Point{
		Size:   req.BuySize,
		Price:  req.BuyPrice,
		Cancel: req.BuyCancelPrice,
	}
	if err := point.Check(); err != nil {
		return nil, err
	}
	if s := point.Side(); s != "BUY" {
		return nil, fmt.Errorf("limit-buy point %v falls on invalid side", point)
	}

	uid := path.Join("/limiters", uuid.New().String())
	buy, err := NewLimiter(uid, product, point)
	if err != nil {
		return nil, err
	}
	if err := kv.WithTransaction(ctx, t.db, buy.save); err != nil {
		return nil, err
	}

	// TODO: We should load and resume these jobs upon restart.

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		if err := buy.Run(t.closeCtx, t.db); err != nil {
			slog.ErrorContext(t.closeCtx, "limit-buy operation has failed", "error", err)
		} else {
			slog.InfoContext(ctx, "limit-buy has completed successfully")
		}
	}()

	resp := &LimitBuyResponse{
		UID: uid,
	}
	return resp, nil
}

func (t *Trader) doLimitSell(ctx context.Context, req *LimitSellRequest) (_ *LimitSellResponse, status error) {
	if req.SellCancelPrice.GreaterThanOrEqual(req.SellPrice) {
		return nil, fmt.Errorf("cancel-price must be lesser than sell-price")
	}

	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "limit-sell has failed", "error", status)
		}
	}()

	// if err := req.Check(); err != nil {
	// 	return err
	// }

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	point := &Point{
		Size:   req.SellSize,
		Price:  req.SellPrice,
		Cancel: req.SellCancelPrice,
	}
	if err := point.Check(); err != nil {
		return nil, err
	}
	if s := point.Side(); s != "SELL" {
		return nil, fmt.Errorf("limit-sell point %v falls on invalid side", point)
	}

	uid := path.Join("/limiters", uuid.New().String())
	sell, err := NewLimiter(uid, product, point)
	if err != nil {
		return nil, err
	}

	if err := kv.WithTransaction(ctx, t.db, sell.save); err != nil {
		return nil, err
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := sell.Run(t.closeCtx, t.db); err != nil {
			slog.ErrorContext(t.closeCtx, "limit-sell operation has failed", "error", err)
		} else {
			slog.InfoContext(ctx, "limit-sell has completed successfully")
		}
	}()

	resp := &LimitSellResponse{
		UID: uid,
	}
	return resp, nil
}

func (t *Trader) doLoop(ctx context.Context, req *LoopRequest) (_ *LoopResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "loop has failed", "error", status)
		}
	}()

	// if err := req.Check(); err != nil {
	// 	return err
	// }

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	buyp := &Point{
		Size:   req.BuySize,
		Price:  req.BuyPrice,
		Cancel: req.BuyCancelPrice,
	}
	if err := buyp.Check(); err != nil {
		return nil, err
	}
	if s := buyp.Side(); s != "BUY" {
		return nil, fmt.Errorf("buy point %v falls on invalid side", buyp)
	}

	sellp := &Point{
		Size:   req.SellSize,
		Price:  req.SellPrice,
		Cancel: req.SellCancelPrice,
	}
	if err := sellp.Check(); err != nil {
		return nil, err
	}
	if s := sellp.Side(); s != "SELL" {
		return nil, fmt.Errorf("sell point %v falls on invalid side", sellp)
	}

	uid := path.Join("/loopers", uuid.New().String())
	loop, err := NewLooper(uid, product, buyp, sellp)
	if err != nil {
		return nil, err
	}
	if err := kv.WithTransaction(ctx, t.db, loop.save); err != nil {
		return nil, err
	}

	// TODO: We should keep track of background jobs

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		if err := loop.Run(t.closeCtx, t.db); err != nil {
			slog.ErrorContext(t.closeCtx, "loop operation has failed", "error", err)
		} else {
			slog.InfoContext(ctx, "loop has completed successfully")
		}
	}()

	resp := &LoopResponse{
		UID: uid,
	}
	return resp, nil
}
