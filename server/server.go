// Copyright (c) 2023 BVK Chaitanya

package server

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
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/dbutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvk/tradebot/trader"
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

	serverStateKey = "/server/state"
)

type Server struct {
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

	traderMap syncmap.Map[string, trader.Job]

	mu sync.Mutex

	state *gobs.ServerState

	exProductsMap map[string]map[string]exchange.Product

	pushoverClient *pushover.Client
}

func New(secrets *Secrets, db kv.Database, opts *Options) (_ *Server, status error) {
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

	var pushoverClient *pushover.Client
	if secrets.Pushover != nil {
		client, err := pushover.New(secrets.Pushover)
		if err != nil {
			return nil, fmt.Errorf("could not create pushover client: %w", err)
		}
		pushoverClient = client
	}

	state, err := dbutil.Get[gobs.ServerState](ctx, db, serverStateKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not load trader state: %w", err)
		}
	}

	t := &Server{
		closeCtx:       ctx,
		closeCause:     cancel,
		db:             db,
		opts:           *opts,
		state:          state,
		exchangeMap:    exchangeMap,
		handlerMap:     make(map[string]http.Handler),
		pushoverClient: pushoverClient,
	}

	if t.state == nil {
		t.state = &gobs.ServerState{
			ExchangeMap: make(map[string]*gobs.ServerExchangeState),
		}
		t.state.ExchangeMap["coinbase"] = &gobs.ServerExchangeState{
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

	// Scan the database and load existing traders.
	if err := kv.WithReader(ctx, t.db, t.loadTrades); err != nil {
		return nil, fmt.Errorf("could not load existing trades: %w", err)
	}

	if err := kv.WithReader(ctx, t.db, t.loadNames); err != nil {
		return nil, fmt.Errorf("could not load job names: %w", err)
	}

	// TODO: Setup a fund

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

func (s *Server) Close() error {
	s.closeCause(fmt.Errorf("trade is closing: %w", os.ErrClosed))
	s.wg.Wait()

	for _, pmap := range s.exProductsMap {
		for _, p := range pmap {
			p.Close()
		}
	}
	for _, exch := range s.exchangeMap {
		exch.Close()
	}
	return nil
}

func (s *Server) HandlerMap() map[string]http.Handler {
	return maps.Clone(s.handlerMap)
}

func (s *Server) Notify(ctx context.Context, at time.Time, msgfmt string, args ...interface{}) {
	if s.pushoverClient != nil {
		if err := s.pushoverClient.SendMessage(ctx, at, fmt.Sprintf(msgfmt, args...)); err != nil {
			log.Printf("warning: could not send pushover message (ignored): %v", err)
		}
	}
}

func (s *Server) Stop(ctx context.Context) error {
	s.jobMap.Range(func(id string, j *job.Job) bool {
		if s := j.State(); !job.IsFinal(s) {
			if err := j.Pause(); err != nil {
				log.Printf("warning: job %s could not be paused (ignored)", id)
			}
		}
		key := path.Join(JobsKeyspace, id)
		state := j.State()
		gstate := &gobs.ServerJobState{CurrentState: string(state), NeedsManualResume: false}
		if err := dbutil.Set(ctx, s.db, key, gstate); err != nil {
			log.Printf("warning: job %s state could not be updated (ignored)", id)
		}
		if job.IsFinal(state) {
			s.jobMap.Delete(id)
		}
		return true
	})

	hostname, _ := os.Hostname()
	s.Notify(ctx, time.Now(), "Trader has stopped gracefully on host named '%s'.", hostname)
	return nil
}

func (s *Server) Start(ctx context.Context) (status error) {
	defer func() {
		hostname, _ := os.Hostname()
		if status == nil {
			s.Notify(ctx, time.Now(), "Trader has started successfully on host named '%s'.", hostname)
		} else {
			s.Notify(ctx, time.Now(), "Trader has failed to start on host named '%s' with error `%v`.", hostname, status)
		}
	}()

	if s.opts.RunFixes {
		if err := s.runFixes(ctx); err != nil {
			return err
		}
	}

	if s.opts.NoResume {
		return nil
	}

	var ids []string
	s.traderMap.Range(func(id string, _ trader.Job) bool {
		ids = append(ids, id)
		return true
	})

	nresumed := 0
	for _, id := range ids {
		j, ok := s.jobMap.Load(id)
		if !ok {
			v, manual, err := s.createJob(ctx, id)
			if err != nil {
				return fmt.Errorf("could not load job %s: %w", id, err)
			}
			if manual || job.IsFinal(v.State()) {
				continue
			}
			j = v
		}

		if err := j.Resume(s.closeCtx); err != nil {
			log.Printf("warning: job %s could not be resumed (ignored)", id)
			continue
		}

		s.jobMap.Store(id, j)
		nresumed++
	}

	log.Printf("%d jobs are resumed", nresumed)
	return nil
}

func (s *Server) runFixes(ctx context.Context) (status error) {
	type Fixer interface {
		Fix(context.Context, *trader.Runtime) error
	}

	s.traderMap.Range(func(id string, v trader.Job) bool {
		if t, ok := v.(Fixer); ok {
			ename, pname := v.ExchangeName(), v.ProductID()
			p, err := s.getProduct(ctx, ename, pname)
			if err != nil {
				log.Printf("could not load product %q in exchange %q: %w", pname, ename, err)
				status = err
				return false
			}
			if err := t.Fix(ctx, &trader.Runtime{Product: p, Database: s.db}); err != nil {
				log.Printf("could not fix %T %v: %w", v, v, err)
				status = err
				return false
			}
			if err := kv.WithReadWriter(ctx, s.db, v.Save); err != nil {
				log.Printf("could not save %T %v: %w", v, v, err)
				status = err
				return false
			}
		}
		return true
	})

	return
}

func (s *Server) getProduct(ctx context.Context, exchangeName, productID string) (exchange.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.getProductLocked(ctx, exchangeName, productID)
}

func (s *Server) getProductLocked(ctx context.Context, exchangeName, productID string) (exchange.Product, error) {
	exch, ok := s.exchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("exchange with name %q not found: %w", exchangeName, os.ErrNotExist)
	}

	if pmap, ok := s.exProductsMap[exchangeName]; ok {
		if p, ok := pmap[productID]; ok {
			return p, nil
		}
	}

	// check if product is enabled.
	estate, ok := s.state.ExchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("exchange %q is not supported")
	}
	if !slices.Contains(estate.EnabledProductIDs, productID) {
		return nil, fmt.Errorf("product %q is not enabled on exchange %q", productID, exchangeName)
	}

	product, err := exch.OpenProduct(s.closeCtx, productID)
	if err != nil {
		return nil, fmt.Errorf("could not open product %q on exchange %q: %w", productID, exchangeName, err)
	}

	pmap, ok := s.exProductsMap[exchangeName]
	if !ok {
		pmap = make(map[string]exchange.Product)
		s.exProductsMap[exchangeName] = pmap
	}

	pmap[productID] = product
	return product, nil
}

func (s *Server) loadProducts(ctx context.Context) (status error) {
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

	for ename, estate := range s.state.ExchangeMap {
		for _, pname := range estate.WatchedProductIDs {
			product, err := s.getProductLocked(ctx, ename, pname)
			if err != nil {
				return fmt.Errorf("could not load exchange %q product %q: %w", ename, pname, err)
			}
			exProductsMap[ename][pname] = product
		}
	}
	s.exProductsMap = exProductsMap
	return nil
}

func (s *Server) loadTrades(ctx context.Context, r kv.Reader) error {
	limiterLoadFunc := func(ctx context.Context, uid string, r kv.Reader) (trader.Job, error) {
		return limiter.Load(ctx, uid, r)
	}
	if err := s.scan(ctx, r, limiter.DefaultKeyspace, limiterLoadFunc); err != nil {
		return fmt.Errorf("could not load all existing limiters: %w", err)
	}

	looperLoadFunc := func(ctx context.Context, uid string, r kv.Reader) (trader.Job, error) {
		return looper.Load(ctx, uid, r)
	}
	if err := s.scan(ctx, r, looper.DefaultKeyspace, looperLoadFunc); err != nil {
		return fmt.Errorf("could not load all existing loopers: %w", err)
	}

	wallerLoadFunc := func(ctx context.Context, uid string, r kv.Reader) (trader.Job, error) {
		return waller.Load(ctx, uid, r)
	}
	if err := s.scan(ctx, r, waller.DefaultKeyspace, wallerLoadFunc); err != nil {
		return fmt.Errorf("could not load all existing wallers: %w", err)
	}
	return nil
}

type traderLoadFunc = func(context.Context, string, kv.Reader) (trader.Job, error)

func (s *Server) scan(ctx context.Context, r kv.Reader, keyspace string, loader traderLoadFunc) error {
	begin := path.Join(keyspace, minUUID)
	end := path.Join(keyspace, maxUUID)

	it, err := r.Ascend(ctx, begin, end)
	if err != nil {
		return err
	}
	defer kv.Close(it)

	for k, _, err := it.Fetch(ctx, false); err == nil; k, _, err = it.Fetch(ctx, true) {
		uid := strings.TrimPrefix(k, keyspace)
		if _, err := uuid.Parse(uid); err != nil {
			continue
		}
		if _, ok := s.traderMap.Load(uid); ok {
			continue
		}

		v, err := loader(ctx, k, r)
		if err != nil {
			return err
		}
		if _, loaded := s.traderMap.LoadOrStore(uid, v); loaded {
			return fmt.Errorf("trader job %s (%T) is already loaded", uid, v)
		}
	}

	if _, _, err := it.Fetch(ctx, false); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (s *Server) loadNames(ctx context.Context, r kv.Reader) error {
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
			s.idNameMap.Store(strings.TrimPrefix(ndata.Data, JobsKeyspace), ndata.Name)
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

func (s *Server) doLimit(ctx context.Context, req *api.LimitRequest) (_ *api.LimitResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "limit request has failed", "error", status)
		}
	}()

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid limit request: %w", err)
	}

	product, err := s.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	limit, err := limiter.New(uid, req.ExchangeName, req.ProductID, req.Point)
	if err != nil {
		return nil, err
	}

	if err := kv.WithReadWriter(ctx, s.db, limit.Save); err != nil {
		return nil, err
	}
	s.traderMap.Store(uid, limit)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return limit.Run(ctx, &trader.Runtime{Product: product, Database: s.db})
	})
	s.jobMap.Store(uid, j)

	if err := j.Resume(s.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.ServerJobState{CurrentState: string(j.State())}
	if err := dbutil.Set(ctx, s.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.LimitResponse{
		UID: uid,
	}
	return resp, nil
}

func (s *Server) doLoop(ctx context.Context, req *api.LoopRequest) (_ *api.LoopResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "loop has failed", "error", status)
		}
	}()

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid loop request: %w", err)
	}

	product, err := s.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	loop, err := looper.New(uid, req.ExchangeName, req.ProductID, req.Buy, req.Sell)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, s.db, loop.Save); err != nil {
		return nil, err
	}
	s.traderMap.Store(uid, loop)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return loop.Run(ctx, &trader.Runtime{Product: product, Database: s.db})
	})
	s.jobMap.Store(uid, j)

	if err := j.Resume(s.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.ServerJobState{CurrentState: string(j.State())}
	if err := dbutil.Set(ctx, s.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.LoopResponse{
		UID: uid,
	}
	return resp, nil
}

func (s *Server) doWall(ctx context.Context, req *api.WallRequest) (_ *api.WallResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "wall has failed", "error", status)
		}
	}()

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid wall request: %w", err)
	}

	product, err := s.getProduct(ctx, req.ExchangeName, req.ProductID)
	if err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	wall, err := waller.New(uid, req.ExchangeName, req.ProductID, req.Pairs)
	if err != nil {
		return nil, err
	}
	if err := kv.WithReadWriter(ctx, s.db, wall.Save); err != nil {
		return nil, err
	}
	s.traderMap.Store(uid, wall)

	j := job.New("" /* state */, func(ctx context.Context) error {
		return wall.Run(ctx, &trader.Runtime{Product: product, Database: s.db})
	})
	s.jobMap.Store(uid, j)

	if err := j.Resume(s.closeCtx); err != nil {
		return nil, err
	}
	gstate := &gobs.ServerJobState{CurrentState: string(j.State())}
	if err := dbutil.Set(ctx, s.db, path.Join(JobsKeyspace, uid), gstate); err != nil {
		return nil, err
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}
