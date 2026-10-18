// Copyright (c) 2023 BVK Chaitanya

package server

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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/etrade"
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
	"github.com/bvk/tradebot/watcher"
	"github.com/bvkgo/kv"
	"github.com/google/uuid"
	"github.com/visvasity/cli"
)

const (
	// We assume MinUUID and MaxUUID are never "generated".
	MinUUID = "00000000-0000-0000-0000-000000000000"
	MaxUUID = "ffffffff-ffff-ffff-ffff-ffffffffffff"

	NamesKeyspace = "/names/"

	ServerStateKey = "/server/state"
)

// ErrUnconfigured error indicates that no exchanges credentials are
// configured.
var ErrUnconfigured = errors.New("unconfigured")

var initServerState = &gobs.ServerState{
	ExchangeMap: map[string]*gobs.ServerExchangeState{
		"coinbase": {
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
		},
		"coinex": {
			EnabledProductIDs: []string{
				"BCHUSDT",
				"BTCUSDT",
				"ETHUSDT",
				"AVAXUSDT",
				"DOGEUSDT",
				"SHIBUSDT",
			},
		},
		"etrade": {
			EnabledProductIDs: []string{
				"TSLA",
			},
		},
	},
}

type Server struct {
	cg ctxutil.CloseGroup

	opts Options

	db kv.Database

	displayRunner exchange.DisplayRunner

	secretsFilePath string

	exchangeMap map[string]exchange.Exchange
	accountMap  map[string]exchange.Account

	handlerMap map[string]http.Handler

	runner *job.Runner

	jobMap syncmap.Map[string, trader.Trader]

	mu sync.Mutex

	state *gobs.ServerState

	accountProductsMap map[string]map[string]exchange.Product

	pushoverClient *pushover.Client

	telegramClient *telegram.Client

	// alertFreezeDeadlineMap holds unique alert key representing an alert to an
	// expiry timestamp before which that specific alert must be suppressed
	// because it was already sent to the user.
	alertFreezeDeadlineMap map[string]time.Time
}

func New(newctx context.Context, secretsFilePath string, db kv.Database, opts *Options) (_ *Server, status error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()
	if err := opts.Check(); err != nil {
		return nil, err
	}

	state, err := kvutil.GetDB[gobs.ServerState](newctx, db, ServerStateKey)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("could not load trader state: %w", err)
		}
		state = initServerState
		if err := kvutil.SetDB(newctx, db, ServerStateKey, state); err != nil {
			return nil, err
		}
	}

	t := &Server{
		db:                     db,
		opts:                   *opts,
		secretsFilePath:        secretsFilePath,
		state:                  state,
		handlerMap:             make(map[string]http.Handler),
		accountProductsMap:     make(map[string]map[string]exchange.Product),
		runner:                 job.NewRunner(db),
		alertFreezeDeadlineMap: make(map[string]time.Time),
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
	t.handlerMap[api.WatchPath] = httpPostJSONHandler(t.doWatch)

	t.handlerMap[api.ExchangeGetOrderPath] = httpPostJSONHandler(t.doExchangeGetOrder)
	t.handlerMap[api.ExchangeGetProductPath] = httpPostJSONHandler(t.doGetProduct)
	t.handlerMap[api.ExchangeUpdateProductPath] = httpPostJSONHandler(t.doExchangeUpdateProduct)

	return t, nil
}

// loadSecrets loads secrets from the secrets file path. If the file doesn't exist,
// it returns an empty Secrets object.
func (s *Server) loadSecrets(ctx context.Context) (*Secrets, error) {
	secrets, err := SecretsFromFile(s.secretsFilePath)
	if err != nil {
		return nil, err
	}
	// Check that secrets exist for at least one exchange and one messaging service..
	if secrets.Coinbase == nil && secrets.CoinEx == nil && secrets.ETrade == nil {
		return nil, fmt.Errorf("no exchange secrets are configured")
	}
	if secrets.Pushover == nil && secrets.Telegram == nil {
		return nil, fmt.Errorf("no messaging service secrets are configured")
	}
	return secrets, nil
}

func (s *Server) Close() error {
	s.cg.Close()

	for _, pmap := range s.accountProductsMap {
		for _, p := range pmap {
			if v, ok := p.(io.Closer); ok {
				v.Close()
			}
		}
	}
	for _, a := range s.accountMap {
		if v, ok := a.(io.Closer); ok {
			v.Close()
		}
	}
	for _, e := range s.exchangeMap {
		if v, ok := e.(io.Closer); ok {
			v.Close()
		}
	}
	return nil
}

func (s *Server) SetDisplayRunner(v exchange.DisplayRunner) {
	s.displayRunner = v
}

func (s *Server) HandlerMap() map[string]http.Handler {
	return maps.Clone(s.handlerMap)
}

func (s *Server) Runtime(product exchange.Product) *trader.Runtime {
	account := s.accountMap[strings.ToLower(product.AccountName())]
	return &trader.Runtime{
		Account:   account,
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
	if err := s.runner.PauseAll(ctx); err != nil {
		return fmt.Errorf("could not stop all jobs: %w", err)
	}
	hostname, _ := os.Hostname()
	s.SendMessage(ctx, time.Now(), "Trader has stopped gracefully on host named '%s'.", hostname)
	return nil
}

// Start initializes exchange and notification clients and resumes trade jobs
// if any. Returns nil if at least one exchange is available and existing
// runnable jobs (if any) are all resumed successfully. Returns ErrUnconfigured
// if no exchange credentials are configured.
func (s *Server) Start(ctx context.Context) (status error) {
	defer func() {
		hostname, _ := os.Hostname()
		if status == nil {
			s.SendMessage(ctx, time.Now(), "Trader has started successfully on host named '%s'.", hostname)
		} else {
			s.SendMessage(ctx, time.Now(), "Trader has failed to start on host named '%s' with error `%v`.", hostname, status)
		}
	}()

	// Load secrets from file
	secrets, err := s.loadSecrets(ctx)
	if err != nil {
		return fmt.Errorf("could not load secrets: %w", err)
	}

	// Create exchange and account objects.
	if len(s.accountMap) == 0 {
		accountMap := make(map[string]exchange.Account)
		exchangeMap := make(map[string]exchange.Exchange)
		defer func() {
			if status != nil {
				for _, a := range accountMap {
					if v, ok := a.(io.Closer); ok {
						v.Close()
					}
				}
				for _, e := range exchangeMap {
					if v, ok := e.(io.Closer); ok {
						v.Close()
					}
				}
			}
		}()

		if secrets.Coinbase != nil {
			cbopts := &coinbase.Options{
				HttpClientTimeout: s.opts.MaxHttpClientTimeout,
			}
			if s.opts.NoFetchCandles {
				cbopts.FetchCandlesInterval = -1
			}
			exchange, err := coinbase.New(ctx, s.db, secrets.Coinbase.KID, secrets.Coinbase.PEM, cbopts)
			if err != nil {
				return fmt.Errorf("could not create coinbase client: %w", err)
			}
			exchangeMap[exchange.ExchangeName()] = exchange
		}

		if secrets.CoinEx != nil {
			opts := &coinex.Options{
				HttpClientTimeout: s.opts.MaxHttpClientTimeout,
			}
			exchange, err := coinex.NewExchange(ctx, secrets.CoinEx.Key, secrets.CoinEx.Secret, opts)
			if err != nil {
				return fmt.Errorf("could not create coinex exchange: %w", err)
			}
			exchangeMap[exchange.ExchangeName()] = exchange
		}

		if secrets.ETrade != nil {
			opts := &etrade.Options{
				DataDir:           s.opts.DataDir,
				HttpClientTimeout: s.opts.MaxHttpClientTimeout,
				Sandbox:           secrets.ETrade.Sandbox,
				DisplayRunner:     s.displayRunner,
			}
			exchange, err := etrade.NewExchange(ctx, s.db, secrets.ETrade, opts)
			if err != nil {
				return fmt.Errorf("could not create etrade exchange: %w", err)
			}
			exchangeMap[exchange.ExchangeName()] = exchange
		}

		for ename, exch := range exchangeMap {
			accounts, err := exch.Accounts(ctx)
			if err != nil {
				return fmt.Errorf("could not list exchange (%s) accounts: %w", ename, err)
			}
			if len(accounts) == 0 {
				slog.Warn("exchange has no usable accounts", "exchange", ename)
				continue
			}
			for _, a := range accounts {
				accountMap[strings.ToLower(a.AccountName())] = a
				log.Printf("added account %s", a.AccountName())
			}
		}

		if len(accountMap) == 0 {
			return fmt.Errorf("no credentials found for any supported exchange: %w", ErrUnconfigured)
		}

		// Fix any missing final-timestamp for filled orders.
		for _, a := range accountMap {
			limiter.RunBackgroundTasks(&s.cg, s.db, a)
		}

		// Configure background watchers for alerts.
		for _, a := range accountMap {
			a := a
			s.cg.Go(func(ctx context.Context) {
				if err := s.watchForLowBalance(ctx, a); err != nil {
					slog.Error("could not alert on low asset balance (check stopped)", "exchange", a.AccountName(), "err", err)
				}
			})
		}
		s.accountMap = accountMap
		s.exchangeMap = exchangeMap
	}

	if err := s.loadProducts(ctx); err != nil {
		return fmt.Errorf("could not load default products: %w", err)
	}

	if s.pushoverClient == nil && secrets.Pushover != nil {
		client, err := pushover.New(secrets.Pushover)
		if err != nil {
			return fmt.Errorf("could not create pushover client: %w", err)
		}
		s.pushoverClient = client
	}

	if s.telegramClient == nil && secrets.Telegram != nil {
		client, err := telegram.New(ctx, s.db, secrets.Telegram)
		if err != nil {
			return fmt.Errorf("could not create telegram client: %w", err)
		}
		s.telegramClient = client

		if err := s.AddTelegramCommand(ctx, "profit", "Prints profit information", s.profitTelegramCmd); err != nil {
			slog.Error("could not add profit telegram command (ignored)", "err", err)
		}
		if len(s.opts.BinaryBackupPath) != 0 {
			if err := s.AddTelegramCommand(ctx, "restart", "Restarts current process", s.restartCmd); err != nil {
				slog.Error("could not add restart telegram command (ignored)", "err", err)
			}
		}
		if err := s.AddTelegramCommand(ctx, "upgrade", "Upgrades tradebot service", s.upgradeCmd); err != nil {
			slog.Error("could not add upgrade telegram command (ignored)", "err", err)
		}
		if err := s.AddTelegramCommand(ctx, "stats", "Prints system and service stats", s.statsCmd); err != nil {
			slog.Error("could not add stats telegram command (ignored)", "err", err)
		}
	}

	if s.opts.RunFixes {
		if err := s.runFixes(ctx); err != nil {
			return err
		}
	}

	if s.opts.NoResume {
		return nil
	}

	traders := make(map[string]trader.Trader)
	collect := func(ctx context.Context, r kv.Reader, jd *gobs.JobData) error {
		uid := jd.ID
		if jd.State.IsDone() {
			log.Printf("job %q is already completed to %q", uid, jd.State)
			return nil
		}

		if jd.Flags&ManualFlag != 0 {
			log.Printf("job %q needs to be resumed manually (flags 0x%x)", uid, jd.Flags)
			return nil
		}

		trader, err := Load(ctx, r, uid, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader job %q: %w", uid, err)
		}
		traders[uid] = trader
		return nil
	}
	if err := s.runner.Scan(ctx, nil /* kv.Reader */, collect); err != nil {
		return fmt.Errorf("could not resume all jobs: %w", err)
	}

	for uid, trader := range traders {
		if err := s.runner.Resume(ctx, uid, s.makeJobFunc(trader), s.cg.Context()); err != nil {
			return fmt.Errorf("could not resume job %q: %w", uid, err)
		}
		log.Printf("resumed job with id %q", uid)
	}

	// Send a profit status notification to telegram upon every successful
	// restart.
	if s.telegramClient != nil {
		var sb strings.Builder
		if err := s.profitTelegramCmd(cli.WithStdout(ctx, &sb), nil); err != nil {
			slog.Warn("could not calculate profit upon every restart (ignored)", "err", err)
		} else {
			s.telegramClient.SendMessage(ctx, time.Time{}, "Current Profit Status\n\n"+sb.String())
		}
	}
	return nil
}

func (s *Server) runFixes(ctx context.Context) (status error) {
	type Fixer interface {
		Fix(context.Context, *trader.Runtime) error
	}

	fix := func(ctx context.Context, r kv.Reader, jd *gobs.JobData) error {
		trader, err := Load(ctx, r, jd.ID, jd.Typename)
		if err != nil {
			return fmt.Errorf("could not load trader %q: %w", jd.ID, err)
		}

		fixer, ok := trader.(Fixer)
		if !ok {
			return nil
		}

		aname, pid := trader.AccountName(), trader.ProductID()
		product, err := s.getProduct(ctx, aname, pid)
		if err != nil {
			return fmt.Errorf("%s: could not load product %q in account %q: %w", jd.ID, pid, aname, err)
		}

		if err := fixer.Fix(ctx, s.Runtime(product)); err != nil {
			return fmt.Errorf("could not fix trader %q: %w", jd.ID, err)
		}
		return nil
	}
	return s.runner.Scan(ctx, nil /* kv.Reader */, fix)
}

func (s *Server) getProduct(ctx context.Context, accountName, productID string) (exchange.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.getProductLocked(ctx, accountName, productID)
}

func (s *Server) getProductLocked(ctx context.Context, accountName, productID string) (exchange.Product, error) {
	accountName = strings.ToLower(accountName)
	account, ok := s.accountMap[accountName]
	if !ok {
		return nil, fmt.Errorf("account with name %q not found: %w", accountName, os.ErrNotExist)
	}

	if pmap, ok := s.accountProductsMap[accountName]; ok {
		if p, ok := pmap[productID]; ok {
			return p, nil
		}
	}

	// check if product is enabled.
	exchangeName := account.ExchangeName()
	estate, ok := s.state.ExchangeMap[exchangeName]
	if !ok {
		return nil, fmt.Errorf("exchange %q is not supported/enabled", exchangeName)
	}
	if !slices.Contains(estate.EnabledProductIDs, productID) {
		return nil, fmt.Errorf("product %q is not enabled on exchange %q", productID, exchangeName)
	}

	product, err := account.OpenSpotProduct(s.cg.Context(), productID)
	if err != nil {
		return nil, fmt.Errorf("could not open product %q on exchange %q: %w", productID, exchangeName, err)
	}

	pmap, ok := s.accountProductsMap[accountName]
	if !ok {
		pmap = make(map[string]exchange.Product)
		s.accountProductsMap[accountName] = pmap
	}

	pmap[productID] = product
	return product, nil
}

func (s *Server) loadProducts(ctx context.Context) (status error) {
	// getProductLocked caches each opened product into s.accountProductsMap
	// (creating the per-account inner map as needed). On failure, close whatever
	// was opened and reset the cache.
	defer func() {
		if status != nil {
			for _, pmap := range s.accountProductsMap {
				for _, product := range pmap {
					product.Close()
				}
			}
			clear(s.accountProductsMap)
		}
	}()

	for aname, account := range s.accountMap {
		ename := account.ExchangeName()
		estate, ok := s.state.ExchangeMap[ename]
		if !ok {
			continue
		}
		for _, pname := range estate.WatchedProductIDs {
			if _, err := s.getProductLocked(ctx, aname, pname); err != nil {
				return fmt.Errorf("could not load account %q product %q: %w", aname, pname, err)
			}
		}
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

	if _, err := s.getProduct(ctx, req.AccountName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	limit, err := limiter.New(uid, req.AccountName, req.ProductID, req.Point)
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
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
		return nil, err
	}

	if err := s.runner.Resume(ctx, uid, s.makeJobFunc(limit), s.cg.Context()); err != nil {
		slog.Error("could not resume newly added limiter job (ignored)", "err", err)
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

	if _, err := s.getProduct(ctx, req.AccountName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	loop, err := looper.New(uid, req.AccountName, req.ProductID, req.Buy, req.Sell)
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
		if req.Pause {
			if err := s.runner.UpdateFlags(ctx, rw, uid, ManualFlag); err != nil {
				slog.Error("could not mark job as paused manually", "err", err)
				return err
			}
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
		return nil, err
	}

	if !req.Pause {
		if err := s.runner.Resume(ctx, uid, s.makeJobFunc(loop), s.cg.Context()); err != nil {
			slog.Error("could not resume newly added looper job (ignored)", "err", err)
		}
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

	if _, err := s.getProduct(ctx, req.AccountName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	wall, err := waller.New(uid, req.AccountName, req.ProductID, req.Pairs)
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
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
		return nil, err
	}

	if err := s.runner.Resume(ctx, uid, s.makeJobFunc(wall), s.cg.Context()); err != nil {
		slog.Error("could not resume newly added waller job (ignored)", "err", err)
	}

	resp := &api.WallResponse{
		UID: uid,
	}
	return resp, nil
}

func (s *Server) doWatch(ctx context.Context, req *api.WatchRequest) (_ *api.WatchResponse, status error) {
	defer func() {
		if status != nil {
			slog.ErrorContext(ctx, "watch job request has failed", "error", status)
		}
	}()

	if err := req.Check(); err != nil {
		return nil, fmt.Errorf("invalid watch job request: %w", err)
	}

	if _, err := s.getProduct(ctx, req.AccountName, req.ProductID); err != nil {
		return nil, err
	}

	uid := uuid.New().String()
	watch, err := watcher.New(uid, req.AccountName, req.ProductID, req.FeePct, req.Pairs)
	if err != nil {
		return nil, err
	}

	start := func(ctx context.Context, rw kv.ReadWriter) error {
		if err := watch.Save(ctx, rw); err != nil {
			return fmt.Errorf("could not save new watcher: %v", err)
		}
		if err := s.runner.Add(ctx, rw, uid, "Watcher"); err != nil {
			return fmt.Errorf("could not add new watcher as a job: %w", err)
		}
		return nil
	}
	if err := kv.WithReadWriter(ctx, s.db, start); err != nil {
		return nil, err
	}

	if err := s.runner.Resume(ctx, uid, s.makeJobFunc(watch), s.cg.Context()); err != nil {
		slog.Error("could not resume newly added watcher job (ignored)", "err", err)
	}

	resp := &api.WatchResponse{
		UID: uid,
	}
	return resp, nil
}
