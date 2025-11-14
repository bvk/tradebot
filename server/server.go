// Copyright (c) 2023 BVK Chaitanya

package server

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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/bvk/tradebot/job"
	"github.com/bvk/tradebot/kvutil"
	"github.com/bvk/tradebot/limiter"
	"github.com/bvk/tradebot/looper"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/syncmap"
	"github.com/bvk/tradebot/telegram"
	"github.com/bvk/tradebot/trader"
	"github.com/bvk/tradebot/waller"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
)

const (
	// We assume MinUUID and MaxUUID are never "generated".
	MinUUID = "00000000-0000-0000-0000-000000000000"
	MaxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	NamesKeyspace = "/names/"

	ServerStateKey = "/server/state"
)

type Server struct {
	cg ctxutil.CloseGroup

	opts Options

	db kv.Database

	secrets *Secrets

	exchangeMap map[string]exchange.Exchange

	handlerMap map[string]http.Handler

	runner *job.Runner

	jobMap syncmap.Map[string, trader.Trader]

	mu sync.Mutex

	state *gobs.ServerState

	exProductsMap map[string]map[string]exchange.Product

	pushoverClient *pushover.Client

	telegramClient *telegram.Client
}

func New(newctx context.Context, secrets *Secrets, db kv.Database, opts *Options) (_ *Server, status error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()
	if err := opts.Check(); err != nil {
		return nil, err
	}

	exchangeMap := make(map[string]exchange.Exchange)
	defer func() {
		if status != nil {
			for _, exch := range exchangeMap {
				exch.Close()
			}
		}
	}()

	if secrets.Coinbase != nil {
		cbopts := &coinbase.Options{
			MaxFetchTimeLatency: opts.MaxFetchTimeLatency,
			HttpClientTimeout:   opts.MaxHttpClientTimeout,
		}
		if opts.NoFetchCandles {
			cbopts.FetchCandlesInterval = -1
		}
		client, err := coinbase.New(newctx, db, secrets.Coinbase.KID, secrets.Coinbase.PEM, cbopts)
		if err != nil {
			return nil, fmt.Errorf("could not create coinbase client: %w", err)
		}
		exchangeMap["coinbase"] = client
	}

	if secrets.CoinEx != nil {
		opts := &coinex.Options{
			HttpClientTimeout: opts.MaxHttpClientTimeout,
		}
		exchange, err := coinex.NewExchange(newctx, secrets.CoinEx.Key, secrets.CoinEx.Secret, opts)
		if err != nil {
			return nil, fmt.Errorf("could not create coinex exchange: %w", err)
		}
		exchangeMap["coinex"] = exchange
	}

	if len(exchangeMap) == 0 {
		return nil, fmt.Errorf("no credentials found for any supported exchange")
	}

	var pushoverClient *pushover.Client
	if secrets.Pushover != nil {
		client, err := pushover.New(secrets.Pushover)
		if err != nil {
			return nil, fmt.Errorf("could not create pushover client: %w", err)
		}
		pushoverClient = client
	}

	var telegramClient *telegram.Client
	if secrets.Telegram != nil {
		client, err := telegram.New(newctx, db, secrets.Telegram)
		if err != nil {
			return nil, fmt.Errorf("could not create telegram client: %w", err)
		}
		telegramClient = client
	}

	state, err := kvutil.GetDB[gobs.ServerState](newctx, db, ServerStateKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not load trader state: %w", err)
		}
	}

	t := &Server{
		db:             db,
		opts:           *opts,
		secrets:        secrets,
		state:          state,
		exchangeMap:    exchangeMap,
		handlerMap:     make(map[string]http.Handler),
		runner:         job.NewRunner(),
		pushoverClient: pushoverClient,
		telegramClient: telegramClient,
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
				"DOGE-USD",
				"SHIB-USD",

				"BCH-USDC",
				"BTC-USDC",
				"ETH-USDC",
				"AVAX-USDC",
				"DOGE-USDC",
				"SHIB-USDC",
			},
		}
	}
	if err := t.loadProducts(newctx); err != nil {
		return nil, fmt.Errorf("could not load default products: %w", err)
	}

	// TODO: Setup a fund

	if err := t.AddTelegramCommand(newctx, "profit", "Prints profit information", t.profitTelegramCmd); err != nil {
		slog.Error("could not add profit telegram command (ignored)", "err", err)
	}
	if len(t.opts.BinaryBackupPath) != 0 {
		if err := t.AddTelegramCommand(newctx, "restart", "Restarts current process", t.restartCmd); err != nil {
			slog.Error("could not add restart telegram command (ignored)", "err", err)
		}
	}
	if err := t.AddTelegramCommand(newctx, "upgrade", "Upgrades tradebot service", t.upgradeCmd); err != nil {
		slog.Error("could not add upgrade telegram command (ignored)", "err", err)
	}
	if err := t.AddTelegramCommand(newctx, "stats", "Prints system and service stats", t.statsCmd); err != nil {
		slog.Error("could not add stats telegram command (ignored)", "err", err)
	}

	t.handlerMap[api.JobListPath] = httpPostJSONHandler(t.doList)
	t.handlerMap[api.JobCancelPath] = httpPostJSONHandler(t.doCancel)
	t.handlerMap[api.JobResumePath] = httpPostJSONHandler(t.doResume)
	t.handlerMap[api.JobPausePath] = httpPostJSONHandler(t.doPause)
	t.handlerMap[api.JobSetOptionPath] = httpPostJSONHandler(t.doJobSetOption)
	t.handlerMap[api.SetJobNamePath] = httpPostJSONHandler(t.doSetJobName)

	t.handlerMap[api.LimitPath] = httpPostJSONHandler(t.doLimit)
	t.handlerMap[api.LoopPath] = httpPostJSONHandler(t.doLoop)
	t.handlerMap[api.WallPath] = httpPostJSONHandler(t.doWall)

	t.handlerMap[api.ExchangeGetOrderPath] = httpPostJSONHandler(t.doExchangeGetOrder)
	t.handlerMap[api.ExchangeGetProductPath] = httpPostJSONHandler(t.doGetProduct)
	t.handlerMap[api.ExchangeUpdateProductPath] = httpPostJSONHandler(t.doExchangeUpdateProduct)

	for _, ex := range t.exchangeMap {
		limiter.RunBackgroundTasks(&t.cg, t.db, ex)
	}
	return t, nil
}

func (s *Server) Close() error {
	s.cg.Close()

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

func (s *Server) Runtime(product exchange.Product) *trader.Runtime {
	exchange := s.exchangeMap[product.ExchangeName()]
	return &trader.Runtime{
		Exchange:  exchange,
		Database:  s.db,
		Product:   product,
		Messenger: s,
	}
}

func (s *Server) SendMessage(ctx context.Context, at time.Time, msgfmt string, args ...interface{}) {
	msg := fmt.Sprintf(msgfmt, args...)
	if s.pushoverClient != nil {
		if err := s.pushoverClient.SendMessage(ctx, at, msg); err != nil {
			slog.Error("could not send pushover message (ignored)", "err", err)
		}
	}
	if s.telegramClient != nil {
		if err := s.telegramClient.SendMessage(ctx, at, msg); err != nil {
			slog.Error("could not send telegram message (ignored)", "err", err)
		}
	}
}

func (s *Server) Stop(ctx context.Context) error {
	if err := job.StopAllDB(ctx, s.runner, s.db); err != nil {
		return fmt.Errorf("could not stop all jobs: %w", err)
	}
	hostname, _ := os.Hostname()
	s.SendMessage(ctx, time.Now(), "Trader has stopped gracefully on host named '%s'.", hostname)
	return nil
}

func (s *Server) Start(ctx context.Context) (status error) {
	defer func() {
		hostname, _ := os.Hostname()
		if status == nil {
			s.SendMessage(ctx, time.Now(), "Trader has started successfully on host named '%s'.", hostname)
		} else {
			s.SendMessage(ctx, time.Now(), "Trader has failed to start on host named '%s' with error `%v`.", hostname, status)
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

	var uids []string
	collect := func(ctx context.Context, r kv.Reader, jd *job.JobData) error {
		uid := jd.UID
		if job.IsDone(jd.State) {
			log.Printf("job %q is already completed to %q", uid, jd.State)
			return nil
		}

		if jd.Flags&ManualFlag != 0 {
			log.Printf("job %q needs to be resumed manually (flags 0x%x)", uid, jd.Flags)
			return nil
		}

		uids = append(uids, uid)
		return nil
	}

	if err := job.ScanDB(ctx, s.runner, s.db, collect); err != nil {
		return fmt.Errorf("could not resume all jobs: %w", err)
	}

	resume := func(ctx context.Context, rw kv.ReadWriter) error {
		for _, uid := range uids {
			jd, err := s.runner.Get(ctx, rw, uid)
			if err != nil {
				return fmt.Errorf("could not get job data for %q: %w", uid, err)
			}
			if _, err := s.resume(ctx, rw, jd); err != nil {
				log.Printf("could not resume job %q (skipped): %v", uid, err)
			}
		}
		return nil
	}
	kv.WithReadWriter(ctx, s.db, resume)
	return nil
}

func (s *Server) resume(ctx context.Context, rw kv.ReadWriter, jdata *job.JobData) (job.State, error) {
	uid := jdata.UID
	if job.IsDone(jdata.State) {
		return "", fmt.Errorf("job %q is already completed (%q)", uid, jdata.State)
	}

	if jdata.Flags&ManualFlag != 0 {
		return "", fmt.Errorf("job %q needs to be resumed manually", uid)
	}

	trader, err := Load(ctx, rw, uid, jdata.Typename)
	if err != nil {
		return "", fmt.Errorf("could not load trader job %q: %w", uid, err)
	}

	state, err := s.runner.Resume(ctx, rw, uid, s.makeJobFunc(trader), s.cg.Context())
	if err != nil {
		return "", fmt.Errorf("could not resume job %q: %w", uid, err)
	}
	log.Printf("resumed job with id %q", uid)
	return state, nil
}

func (s *Server) runFixes(ctx context.Context) (status error) {
	type Fixer interface {
		Fix(context.Context, *trader.Runtime) error
	}

	fix := func(ctx context.Context, r kv.Reader, jd *job.JobData) error {
		trader, err := Load(ctx, r, jd.UID, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader %q: %w", jd.UID, err)
		}

		fixer, ok := trader.(Fixer)
		if !ok {
			return nil
		}

		ename, pid := trader.ExchangeName(), trader.ProductID()
		product, err := s.getProduct(ctx, ename, pid)
		if err != nil {
			return fmt.Errorf("%s: could not load product %q in exchange %q: %w", jd.UID, pid, ename, err)
		}

		if err := fixer.Fix(ctx, s.Runtime(product)); err != nil {
			return fmt.Errorf("could not fix trader %q: %w", jd.UID, err)
		}
		return nil
	}
	return job.ScanDB(ctx, s.runner, s.db, fix)
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
		return nil, fmt.Errorf("exchange %q is not supported", exchangeName)
	}
	if !slices.Contains(estate.EnabledProductIDs, productID) {
		return nil, fmt.Errorf("product %q is not enabled on exchange %q", productID, exchangeName)
	}

	product, err := exch.OpenSpotProduct(s.cg.Context(), productID)
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

	if _, err := s.getProduct(ctx, req.ExchangeName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	limit, err := limiter.New(uid, req.ExchangeName, req.ProductID, req.Point)
	if err != nil {
		return nil, err
	}

	start := func(ctx context.Context, rw kv.ReadWriter) error {
		if err := limit.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save new limiter: %v", err)
		}
		if err := s.runner.Add(ctx, rw, uid, "Limiter"); err != nil {
			return fmt.Errorf("could not add new limiter as a job: %w", err)
		}
		if _, err := s.runner.Resume(ctx, rw, uid, s.makeJobFunc(limit), s.cg.Context()); err != nil {
			return fmt.Errorf("could not resume new limiter job: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
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

	if _, err := s.getProduct(ctx, req.ExchangeName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	loop, err := looper.New(uid, req.ExchangeName, req.ProductID, req.Buy, req.Sell)
	if err != nil {
		return nil, err
	}

	start := func(ctx context.Context, rw kv.ReadWriter) error {
		if err := loop.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save new looper: %v", err)
		}
		if err := s.runner.Add(ctx, rw, uid, "Looper"); err != nil {
			return fmt.Errorf("could not add new looper as a job: %w", err)
		}
		if _, err := s.runner.Resume(ctx, rw, uid, s.makeJobFunc(loop), s.cg.Context()); err != nil {
			return fmt.Errorf("could not resume new looper job: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
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

	if _, err := s.getProduct(ctx, req.ExchangeName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	wall, err := waller.New(uid, req.ExchangeName, req.ProductID, req.Pairs)
	if err != nil {
		return nil, err
	}

	start := func(ctx context.Context, rw kv.ReadWriter) error {
		if err := wall.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save new waller: %v", err)
		}
		if err := s.runner.Add(ctx, rw, uid, "Waller"); err != nil {
			return fmt.Errorf("could not add new waller as a job: %w", err)
		}
		if _, err := s.runner.Resume(ctx, rw, uid, s.makeJobFunc(wall), s.cg.Context()); err != nil {
			return fmt.Errorf("could not resume new waller job: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
		return nil, err
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}
