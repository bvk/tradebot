// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/gob"
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
	"slices"
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
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

const (
	// We assume minUUID and maxUUID are never "generated".
	minUUID = "00000000-0000-0000-0000-000000000000"
	maxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	JobsKeyspace    = "/jobs/"
	NamesKeyspace   = "/names/"
	CandlesKeyspace = "/candles/"

	traderStateKey = "/trader/state"
)

type Trader struct {
	closeCtx   context.Context
	closeCause context.CancelCauseFunc

	wg sync.WaitGroup

	opts Options

	db kv.Database

	exchangeMap map[string]exchange.Exchange

	handlerMap map[string]http.Handler

	// jobMap holds all running (or completed) jobs that are *not* manually
	// paused or canceled. If a job is manually paused or canceled, it is be
	// removed from this map. TODO: We could periodically scan-and-remove
	// completed jobs.
	jobMap syncmap.Map[string, *job.Job]

	idNameMap syncmap.Map[string, string]

	limiterMap syncmap.Map[string, *limiter.Limiter]
	looperMap  syncmap.Map[string, *looper.Looper]
	wallerMap  syncmap.Map[string, *waller.Waller]

	mu sync.Mutex

	state *gobs.TraderState

	exProductsMap map[string]map[string]exchange.Product
}

func NewTrader(secrets *Secrets, db kv.Database, opts *Options) (_ *Trader, status error) {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer func() {
		if status != nil {
			cancel(status)
		}
	}()

	exchangeMap := make(map[string]exchange.Exchange)
	defer func() {
		if status != nil {
			for _, exch := range exchangeMap {
				exch.Close()
			}
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
	exchangeMap["coinbase"] = coinbaseClient

	state, err := dbutil.Get[gobs.TraderState](ctx, db, traderStateKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not load trader state: %w", err)
		}
	}

	t := &Trader{
		closeCtx:    ctx,
		closeCause:  cancel,
		db:          db,
		opts:        *opts,
		state:       state,
		exchangeMap: exchangeMap,
		handlerMap:  make(map[string]http.Handler),
	}

	if t.state == nil {
		t.state = &gobs.TraderState{
			ExchangeMap: make(map[string]*gobs.TraderExchangeState),
		}
		t.state.ExchangeMap["coinbase"] = &gobs.TraderExchangeState{
			EnabledProductIDs: []string{
				"BCH-USD",
				"BTC-USD",
				"ETH-USD",
				"AVAX-USD",
			},
		}
	}
	if err := t.loadProducts(ctx); err != nil {
		return nil, fmt.Errorf("could not load default products: %w", err)
	}

	t.handlerMap[api.JobListPath] = httpPostJSONHandler(t.doList)
	t.handlerMap[api.JobCancelPath] = httpPostJSONHandler(t.doCancel)
	t.handlerMap[api.JobResumePath] = httpPostJSONHandler(t.doResume)
	t.handlerMap[api.JobPausePath] = httpPostJSONHandler(t.doPause)
	t.handlerMap[api.JobRenamePath] = httpPostJSONHandler(t.doRename)

	t.handlerMap[api.LimitPath] = httpPostJSONHandler(t.doLimit)
	t.handlerMap[api.LoopPath] = httpPostJSONHandler(t.doLoop)
	t.handlerMap[api.WallPath] = httpPostJSONHandler(t.doWall)

	t.handlerMap[api.ExchangeGetOrderPath] = httpPostJSONHandler(t.doExchangeGetOrder)
	t.handlerMap[api.ExchangeGetProductPath] = httpPostJSONHandler(t.doGetProduct)
	t.handlerMap[api.ExchangeGetCandlesPath] = httpPostJSONHandler(t.doGetCandles)

	return t, nil
}

func (t *Trader) Close() error {
	t.closeCause(fmt.Errorf("trade is closing: %w", os.ErrClosed))
	t.wg.Wait()

	for _, pmap := range t.exProductsMap {
		for _, p := range pmap {
			p.Close()
		}
	}
	for _, exch := range t.exchangeMap {
		exch.Close()
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
		state := j.State()
		gstate := &gobs.TraderJobState{CurrentState: string(state), NeedsManualResume: false}
		if err := dbutil.Set(ctx, t.db, key, gstate); err != nil {
			log.Printf("warning: job %s state could not be updated (ignored)", id)
		}
		if job.IsFinal(state) {
			t.jobMap.Delete(id)
		}
		return true
	})

	return nil
}

func (t *Trader) Start(ctx context.Context) error {
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

func (t *Trader) getProduct(ctx context.Context, exchangeName, productID string) (exchange.Product, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.getProductLocked(ctx, exchangeName, productID)
}

func (t *Trader) getProductLocked(ctx context.Context, exchangeName, productID string) (exchange.Product, error) {
	exch, ok := t.exchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("exchange with name %q not found: %w", exchangeName, os.ErrNotExist)
	}

	if pmap, ok := t.exProductsMap[exchangeName]; ok {
		if p, ok := pmap[productID]; ok {
			return p, nil
		}
	}

	// check if product is enabled.
	estate, ok := t.state.ExchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("exchange %q is not supported")
	}
	if !slices.Contains(estate.EnabledProductIDs, productID) {
		return nil, fmt.Errorf("product %q is not enabled on exchange %q", productID, exchangeName)
	}

	product, err := exch.OpenProduct(t.closeCtx, productID)
	if err != nil {
		return nil, fmt.Errorf("could not open product %q on exchange %q: %w", productID, exchangeName, err)
	}

	pmap, ok := t.exProductsMap[exchangeName]
	if !ok {
		pmap = make(map[string]exchange.Product)
		t.exProductsMap[exchangeName] = pmap
	}

	pmap[productID] = product
	return product, nil
}

func (t *Trader) loadProducts(ctx context.Context) (status error) {
	exProductsMap := make(map[string]map[string]exchange.Product)
	defer func() {
		if status != nil {
			for _, pmap := range exProductsMap {
				for _, product := range pmap {
					product.Close()
				}
			}
		}
	}()

	for ename, estate := range t.state.ExchangeMap {
		for _, pname := range estate.WatchedProductIDs {
			product, err := t.getProductLocked(ctx, ename, pname)
			if err != nil {
				return fmt.Errorf("could not load exchange %q product %q: %w", ename, pname, err)
			}
			exProductsMap[ename][pname] = product
		}
	}
	t.exProductsMap = exProductsMap
	return nil
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
	if err := t.loadNames(ctx, r); err != nil {
		return fmt.Errorf("could not load all existing names: %w", err)
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
		uid := strings.TrimPrefix(k, limiter.DefaultKeyspace)
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
		uid := strings.TrimPrefix(k, looper.DefaultKeyspace)
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
		uid := strings.TrimPrefix(k, waller.DefaultKeyspace)
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

func (t *Trader) loadNames(ctx context.Context, r kv.Reader) error {
	begin := path.Join(NamesKeyspace, minUUID)
	end := path.Join(NamesKeyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return fmt.Errorf("could not create iterator for names keyspace: %w", err)
	}
	defer kv.Close(it)

	for k, v, err := it.Fetch(ctx, false); err == nil; k, v, err = it.Fetch(ctx, true) {
		uid := strings.TrimPrefix(k, NamesKeyspace)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}

		ndata := new(gobs.NameData)
		if err := gob.NewDecoder(v).Decode(ndata); err != nil {
			return fmt.Errorf("could not decode name data: %w", err)
		}

		if strings.HasPrefix(ndata.Data, JobsKeyspace) {
			t.idNameMap.Store(strings.TrimPrefix(ndata.Data, JobsKeyspace), ndata.Name)
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

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid limit request: %w", err)
	}

	product, err := t.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	limit, err := limiter.New(uid, req.ExchangeName, req.ProductID, req.Point)
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
	gstate := &gobs.TraderJobState{CurrentState: string(j.State())}
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
		return nil, fmt.Errorf("invalid loop request: %w", err)
	}

	product, err := t.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	loop, err := looper.New(uid, req.ExchangeName, req.ProductID, req.Buy, req.Sell)
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
	gstate := &gobs.TraderJobState{CurrentState: string(j.State())}
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

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid wall request: %w", err)
	}

	product, err := t.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	wall, err := waller.New(uid, req.ExchangeName, req.ProductID, req.Pairs)
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
	gstate := &gobs.TraderJobState{CurrentState: string(j.State())}
	if err := dbutil.Set(ctx, t.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}
