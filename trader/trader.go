// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/bvkgo/kv"
	"github.com/bvkgo/tradebot/api"
	"github.com/bvkgo/tradebot/coinbase"
	"github.com/bvkgo/tradebot/dbutil"
	"github.com/bvkgo/tradebot/exchange"
	"github.com/bvkgo/tradebot/job"
	"github.com/bvkgo/tradebot/kvutil"
	"github.com/bvkgo/tradebot/limiter"
	"github.com/bvkgo/tradebot/looper"
	"github.com/bvkgo/tradebot/point"
	"github.com/bvkgo/tradebot/syncmap"
	"github.com/bvkgo/tradebot/waller"
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

	handlerMap map[string]http.Handler

	jobMap     syncmap.Map[string, *job.Job]
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
			return nil, err
		}
		coinbaseClient = client
	}

	t := &Trader{
		closeCtx:       ctx,
		closeCause:     cancel,
		coinbaseClient: coinbaseClient,
		db:             db,
		opts:           *opts,
		handlerMap:     make(map[string]http.Handler),
		productMap:     make(map[string]exchange.Product),
	}

	// FIXME: We need to find a cleaner way to load default products. We need it
	// before REST api is enabled.

	t.handlerMap["/trader/list"] = httpPostJSONHandler(t.doList)
	t.handlerMap["/trader/cancel"] = httpPostJSONHandler(t.doCancel)
	t.handlerMap["/trader/resume"] = httpPostJSONHandler(t.doResume)
	t.handlerMap["/trader/pause"] = httpPostJSONHandler(t.doPause)
	t.handlerMap["/trader/loop"] = httpPostJSONHandler(t.doLoop)
	t.handlerMap["/trader/limit"] = httpPostJSONHandler(t.doLimit)
	t.handlerMap["/trader/wall"] = httpPostJSONHandler(t.doWall)

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
		state := j.State()
		if err := dbutil.SetString(ctx, t.db, key, state); err != nil {
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

	count := 0
	t.jobMap.Range(func(id string, job *job.Job) bool {
		count++
		if err := job.Resume(t.closeCtx); err != nil {
			log.Printf("warning: job %s could not be resumed (ignored)", id)
			return false
		}
		return true
	})
	log.Printf("%d jobs are resumed", count)

	return nil
}

func (t *Trader) getProduct(ctx context.Context, name string) (exchange.Product, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

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

	for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}

		l, err := limiter.Load(ctx, k, r, t.productMap)
		if err != nil {
			return err
		}

		if _, loaded := t.limiterMap.LoadOrStore(uid, l); loaded {
			return fmt.Errorf("limiter %s is already loaded", uid)
		}

		// Create a job for the limiter.
		state, err := kvutil.GetString[job.State](ctx, r, path.Join(JobsKeyspace, uid))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if job.IsFinal(state) {
			continue
		}

		j := job.New(job.State(state), func(ctx context.Context) error {
			return l.Run(ctx, t.db)
		})
		if _, loaded := t.jobMap.LoadOrStore(uid, j); loaded {
			return fmt.Errorf("limiter job for %s already exists", uid)
		}
	}

	if err := it.Err(); err != nil {
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

	for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}

		l, err := looper.Load(ctx, k, r, t.productMap)
		if err != nil {
			return err
		}
		if _, loaded := t.looperMap.LoadOrStore(uid, l); loaded {
			return fmt.Errorf("looper %s is already loaded", uid)
		}

		// Create a job for the looper.
		state, err := kvutil.GetString[job.State](ctx, r, path.Join(JobsKeyspace, uid))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if job.IsFinal(state) {
			continue
		}
		j := job.New(job.State(state), func(ctx context.Context) error {
			return l.Run(ctx, t.db)
		})
		if _, loaded := t.jobMap.LoadOrStore(uid, j); loaded {
			return fmt.Errorf("looper job for %s already exists", uid)
		}
	}

	if err := it.Err(); err != nil {
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

	for k, _, ok := it.Current(ctx); ok; k, _, ok = it.Next(ctx) {
		_, uid := path.Split(k)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}

		w, err := waller.Load(ctx, k, r, t.productMap)
		if err != nil {
			return err
		}
		if _, loaded := t.wallerMap.LoadOrStore(uid, w); loaded {
			return fmt.Errorf("waller %s is already loaded", uid)
		}

		// Create a job for the waller.
		state, err := kvutil.GetString[job.State](ctx, r, path.Join(JobsKeyspace, uid))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if job.IsFinal(state) {
			continue
		}

		j := job.New(job.State(state), func(ctx context.Context) error {
			return w.Run(ctx, t.db)
		})
		if _, loaded := t.jobMap.LoadOrStore(uid, j); loaded {
			return fmt.Errorf("waller job for %s already exists", uid)
		}
	}

	if err := it.Err(); err != nil {
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

func (t *Trader) doPause(ctx context.Context, req *api.PauseRequest) (*api.PauseResponse, error) {
	key := path.Join(JobsKeyspace, req.UID)
	state, err := dbutil.GetString[job.State](ctx, t.db, key)
	if err != nil {
		return nil, err
	}
	if !job.IsFinal(state) {
		j, ok := t.jobMap.Load(req.UID)
		if !ok {
			return nil, fmt.Errorf("job map has no job %s", req.UID)
		}
		if err := j.Pause(); err != nil {
			return nil, err
		}
		state = j.State()
		if err := dbutil.SetString(ctx, t.db, key, state); err != nil {
			return nil, err
		}
	}
	resp := &api.PauseResponse{
		FinalState: string(state),
	}
	return resp, nil
}

func (t *Trader) doResume(ctx context.Context, req *api.ResumeRequest) (*api.ResumeResponse, error) {
	key := path.Join(JobsKeyspace, req.UID)
	state, err := dbutil.GetString[job.State](ctx, t.db, key)
	if err != nil {
		return nil, err
	}
	if !job.IsFinal(state) {
		j, ok := t.jobMap.Load(req.UID)
		if !ok {
			return nil, fmt.Errorf("job map has no job %s", req.UID)
		}
		if err := j.Resume(t.closeCtx); err != nil {
			return nil, err
		}
		state = j.State()
		if err := dbutil.SetString(ctx, t.db, key, state); err != nil {
			return nil, err
		}
	}
	resp := &api.ResumeResponse{
		FinalState: string(state),
	}
	return resp, nil
}

func (t *Trader) doCancel(ctx context.Context, req *api.CancelRequest) (*api.CancelResponse, error) {
	key := path.Join(JobsKeyspace, req.UID)
	state, err := dbutil.GetString[job.State](ctx, t.db, key)
	if err != nil {
		return nil, err
	}
	if !job.IsFinal(state) {
		j, ok := t.jobMap.Load(req.UID)
		if !ok {
			return nil, fmt.Errorf("job map has no job %s", req.UID)
		}
		if err := j.Cancel(); err != nil {
			return nil, err
		}
		state = j.State()
		if err := dbutil.SetString(ctx, t.db, key, state); err != nil {
			return nil, err
		}
	}
	resp := &api.CancelResponse{
		FinalState: string(state),
	}
	return resp, nil
}

func (t *Trader) doList(ctx context.Context, req *api.ListRequest) (*api.ListResponse, error) {
	getState := func(id string) job.State {
		if j, ok := t.jobMap.Load(id); ok {
			return j.State()
		}
		key := path.Join(JobsKeyspace, id)
		v, err := dbutil.GetString[job.State](ctx, t.db, key)
		if err != nil {
			log.Printf("could not fetch job state for %s (ignored): %v", id, err)
			return ""
		}
		return v
	}

	resp := new(api.ListResponse)
	t.limiterMap.Range(func(id string, l *limiter.Limiter) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Limiter",
			State: string(getState(id)),
		})
		return true
	})
	t.looperMap.Range(func(id string, l *looper.Looper) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Looper",
			State: string(getState(id)),
		})
		return true
	})
	t.wallerMap.Range(func(id string, w *waller.Waller) bool {
		resp.Jobs = append(resp.Jobs, &api.ListResponseItem{
			UID:   id,
			Type:  "Waller",
			State: string(getState(id)),
		})
		return true
	})
	return resp, nil
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
	limit, err := limiter.New(key, product, point)
	if err != nil {
		return nil, err
	}

	if err := kv.WithReadWriter(ctx, t.db, limit.Save); err != nil {
		return nil, err
	}
	t.limiterMap.Store(uid, limit)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return limit.Run(ctx, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	if err := dbutil.SetString(ctx, t.db, path.Join(JobsKeyspace, uid), j.State()); err != nil {
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
	loop, err := looper.New(key, product, &req.Buy, &req.Sell)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, t.db, loop.Save); err != nil {
		return nil, err
	}
	t.looperMap.Store(uid, loop)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return loop.Run(ctx, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	if err := dbutil.SetString(ctx, t.db, path.Join(JobsKeyspace, uid), j.State()); err != nil {
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
	wall, err := waller.New(key, product, buys, sells)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, t.db, wall.Save); err != nil {
		return nil, err
	}
	t.wallerMap.Store(uid, wall)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return wall.Run(ctx, t.db)
	})
	t.jobMap.Store(uid, j)

	if err := j.Resume(t.closeCtx); err != nil {
		return nil, err
	}
	if err := dbutil.SetString(ctx, t.db, path.Join(JobsKeyspace, uid), j.State()); err != nil {
		return nil, err
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}
