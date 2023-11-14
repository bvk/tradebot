// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/dbutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/point"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

const (
	// We assume minUUID and maxUUID are never "generated".
	minUUID = "00000000-0000-0000-0000-000000000000"
	maxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	JobsKeyspace = "/jobs"
)

type Trader struct {
	closeCtx   context.Context
	closeCause context.CancelCauseFunc

	wg sync.WaitGroup

	opts Options

	db kv.Database

	coinbaseClient *coinbase.Client

	exchangeMap map[string]exchange.Exchange

	handlerMap map[string]http.Handler

	// jobMap holds all running (or completed) jobs that are *not* manually
	// paused or canceled. If a job is manually paused or canceled, it is be
	// removed from this map. TODO: We could periodically scan-and-remove
	// completed jobs.
	jobMap syncmap.Map[string, *job.Job]

	limiterMap syncmap.Map[string, *limiter.Limiter]
	looperMap  syncmap.Map[string, *looper.Looper]
	wallerMap  syncmap.Map[string, *waller.Waller]

	mu sync.Mutex

	productMap map[string]exchange.Product
}

func NewTrader(secrets *Secrets, db kv.Database, opts *Options) (_ *Trader, status error) {
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
			return nil, fmt.Errorf("could not create coinbase client: %w", err)
		}
		coinbaseClient = client
	}

	t := &Trader{
		closeCtx:       ctx,
		closeCause:     cancel,
		coinbaseClient: coinbaseClient,
		db:             db,
		opts:           *opts,
		exchangeMap:    make(map[string]exchange.Exchange),
		handlerMap:     make(map[string]http.Handler),
		productMap:     make(map[string]exchange.Product),
	}
	t.exchangeMap["coinbase"] = coinbaseClient

	// FIXME: We need to find a cleaner way to load default products. We need it
	// before REST api is enabled.

	t.handlerMap["/trader/list"] = httpPostJSONHandler(t.doList)
	t.handlerMap["/trader/cancel"] = httpPostJSONHandler(t.doCancel)
	t.handlerMap["/trader/resume"] = httpPostJSONHandler(t.doResume)
	t.handlerMap["/trader/pause"] = httpPostJSONHandler(t.doPause)
	t.handlerMap["/trader/loop"] = httpPostJSONHandler(t.doLoop)
	t.handlerMap["/trader/limit"] = httpPostJSONHandler(t.doLimit)
	t.handlerMap["/trader/wall"] = httpPostJSONHandler(t.doWall)

	t.handlerMap["/exchange/get-order"] = httpPostJSONHandler(t.doExchangeGetOrder)

	// TODO: Resume existing traders.

	return t, nil
}

func (t *Trader) Close() error {
	t.closeCause(fmt.Errorf("trade is closing: %w", os.ErrClosed))
	t.wg.Wait()

	if t.coinbaseClient != nil {
		for _, p := range t.productMap {
			t.coinbaseClient.CloseProduct(p.(*coinbase.Product))
		}
		t.coinbaseClient.Close()
	}
	return nil
}

func (t *Trader) HandlerMap() map[string]http.Handler {
	return maps.Clone(t.handlerMap)
}

func (t *Trader) Stop(ctx context.Context) error {
	t.jobMap.Range(func(id string, j *job.Job) bool {
		if s := j.State(); !job.IsFinal(s) {
			if err := j.Pause(); err != nil {
				log.Printf("warning: job %s could not be paused (ignored)", id)
			}
		}
		key := path.Join(JobsKeyspace, id)
		gstate := &gobs.TraderJobState{State: j.State(), NeedsManualResume: false}
		log.Printf("job %v state is changed to %s", id, gstate.State)
		if err := dbutil.Set(ctx, t.db, key, gstate); err != nil {
			log.Printf("warning: job %s state could not be updated (ignored)", id)
		}
		if job.IsFinal(gstate.State) {
			t.jobMap.Delete(id)
		}
		return true
	})

	return nil
}

func (t *Trader) Start(ctx context.Context) error {
	// Load default set of products.
	defaultProducts := []string{"BCH-USD"}

	for _, p := range defaultProducts {
		if _, err := t.getProduct(ctx, p); err != nil {
			return err
		}
	}

	// Scan the database and load existing traders.
	if err := kv.WithReader(ctx, t.db, t.loadTrades); err != nil {
		return err
	}

	if t.opts.NoResume {
		return nil
	}

	var ids []string
	t.limiterMap.Range(func(id string, l *limiter.Limiter) bool {
		ids = append(ids, id)
		return true
	})
	t.looperMap.Range(func(id string, l *looper.Looper) bool {
		ids = append(ids, id)
		return true
	})
	t.wallerMap.Range(func(id string, w *waller.Waller) bool {
		ids = append(ids, id)
		return true
	})

	nresumed := 0
	for _, id := range ids {
		j, ok := t.jobMap.Load(id)
		if !ok {
			v, manual, err := t.createJob(ctx, id)
			if err != nil {
				return fmt.Errorf("could not load job %s: %w", id, err)
			}
			if manual || job.IsFinal(v.State()) {
				continue
			}
			j = v
		}

		if err := j.Resume(t.closeCtx); err != nil {
			log.Printf("warning: job %s could not be resumed (ignored)", id)
			continue
		}

		t.jobMap.Store(id, j)
		nresumed++
	}

	log.Printf("%d jobs are resumed", nresumed)
	return nil
}

func (t *Trader) getProduct(ctx context.Context, name string) (exchange.Product, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	product, ok := t.productMap[name]
	if !ok {
		p, err := t.coinbaseClient.NewProduct(t.closeCtx, name)
		if err != nil {
			return nil, err
		}
		product = p
		t.productMap[name] = p
	}
	return product, nil
}

func (t *Trader) loadTrades(ctx context.Context, r kv.Reader) error {
	if err := t.loadLimiters(ctx, r); err != nil {
		return fmt.Errorf("could not load all existing limiters: %w", err)
	}
	if err := t.loadLoopers(ctx, r); err != nil {
		return fmt.Errorf("could not load all existing loopers: %w", err)
	}
	if err := t.loadWallers(ctx, r); err != nil {
		return fmt.Errorf("could not load all existing wallers: %w", err)
	}
	return nil
}

func (t *Trader) loadLimiters(ctx context.Context, r kv.Reader) error {
	begin := path.Join(limiter.DefaultKeyspace, minUUID)
	end := path.Join(limiter.DefaultKeyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return err
	}
	defer kv.Close(it)

	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}
		if _, ok := t.limiterMap.Load(uid); ok {
			continue
		}

		l, err := limiter.Load(ctx, k, r)
		if err != nil {
			return err
		}
		if _, loaded := t.limiterMap.LoadOrStore(uid, l); loaded {
			return fmt.Errorf("limiter %s is already loaded", uid)
		}
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (t *Trader) loadLoopers(ctx context.Context, r kv.Reader) error {
	begin := path.Join(looper.DefaultKeyspace, minUUID)
	end := path.Join(looper.DefaultKeyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return err
	}
	defer kv.Close(it)

	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}
		if _, ok := t.looperMap.Load(uid); ok {
			continue
		}

		l, err := looper.Load(ctx, k, r)
		if err != nil {
			return err
		}
		if _, loaded := t.looperMap.LoadOrStore(uid, l); loaded {
			return fmt.Errorf("looper %s is already loaded", uid)
		}
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (t *Trader) loadWallers(ctx context.Context, r kv.Reader) error {
	begin := path.Join(waller.DefaultKeyspace, minUUID)
	end := path.Join(waller.DefaultKeyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return err
	}
	defer kv.Close(it)

	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}
		if _, ok := t.wallerMap.Load(uid); ok {
			continue
		}

		w, err := waller.Load(ctx, k, r)
		if err != nil {
			return err
		}
		if _, loaded := t.wallerMap.LoadOrStore(uid, w); loaded {
			return fmt.Errorf("waller %s is already loaded", uid)
		}
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
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
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
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

func (t *Trader) doLimit(ctx context.Context, req *api.LimitRequest) (_ *api.LimitResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "limit request has failed", "error", status)
		}
	}()

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	point := &point.Point{
		Size:   req.Size,
		Price:  req.Price,
		Cancel: req.CancelPrice,
	}
	if err := point.Check(); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	key := path.Join(limiter.DefaultKeyspace, uid)
	limit, err := limiter.New(key, product.ID(), point)
	if err != nil {
		return nil, err
	}

	if err := kv.WithReadWriter(ctx, t.db, limit.Save); err != nil {
		return nil, err
	}
	t.limiterMap.Store(uid, limit)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return limit.Run(ctx, product, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.TraderJobState{State: j.State()}
	if err := dbutil.Set(ctx, t.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.LimitResponse{
		UID:  uid,
		Side: limit.Side(),
	}
	return resp, nil
}

func (t *Trader) doLoop(ctx context.Context, req *api.LoopRequest) (_ *api.LoopResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "loop has failed", "error", status)
		}
	}()

	if err := req.Check(); err != nil {
		return nil, err
	}

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	key := path.Join(looper.DefaultKeyspace, uid)
	loop, err := looper.New(key, req.Product, &req.Buy, &req.Sell)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, t.db, loop.Save); err != nil {
		return nil, err
	}
	t.looperMap.Store(uid, loop)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return loop.Run(ctx, product, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.TraderJobState{State: j.State()}
	if err := dbutil.Set(ctx, t.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.LoopResponse{
		UID: uid,
	}
	return resp, nil
}

func (t *Trader) doWall(ctx context.Context, req *api.WallRequest) (_ *api.WallResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "wall has failed", "error", status)
		}
	}()

	// if err := req.Check(); err != nil {
	// 	return err
	// }

	var buys, sells []*point.Point
	for i, bs := range req.BuySellPoints {
		buy, sell := bs[0], bs[1]
		if err := buy.Check(); err != nil {
			return nil, fmt.Errorf("buy point %d (%v) is invalid", i, buy)
		}
		if side := buy.Side(); side != "BUY" {
			return nil, fmt.Errorf("buy point %d falls on invalid side", buy)
		}
		if err := sell.Check(); err != nil {
			return nil, fmt.Errorf("sell point %d (%v) is invalid", i, sell)
		}
		if side := sell.Side(); side != "SELL" {
			return nil, fmt.Errorf("sell point %d falls on invalid side", sell)
		}
		buys = append(buys, buy)
		sells = append(sells, sell)
	}

	product, err := t.getProduct(ctx, req.Product)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	key := path.Join(waller.DefaultKeyspace, uid)
	wall, err := waller.New(key, req.Product, buys, sells)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, t.db, wall.Save); err != nil {
		return nil, err
	}
	t.wallerMap.Store(uid, wall)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return wall.Run(ctx, product, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.TraderJobState{State: j.State()}
	if err := dbutil.Set(ctx, t.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}
