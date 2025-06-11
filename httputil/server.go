// Copyright (c) 2023 BVK Chaitanya

package httputil

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bvk/tradebot/syncmap"
	"github.com/google/uuid"
)

type Server struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	opts Options

	nextServerID atomic.Int64
	serverMap    syncmap.Map[int64, *http.Server]

	mux atomic.Pointer[http.ServeMux]

	mutex      sync.Mutex
	handlerMap map[string]http.Handler
}

// New creates a http server.
func New(opts *Options) (_ *Server, status error) {
	if opts == nil {
		opts = new(Options)
	}
	opts.setDefaults()
	if err := opts.Check(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer func() {
		if status != nil {
			cancel(status)
		}
	}()

	s := &Server{
		ctx:        ctx,
		cancel:     cancel,
		opts:       *opts,
		handlerMap: make(map[string]http.Handler),
	}
	return s, nil
}

func (s *Server) Close() error {
	s.cancel(os.ErrClosed)
	s.serverMap.Range(func(id int64, svr *http.Server) bool {
		svr.Close()
		return true
	})
	s.wg.Wait()
	return nil
}

func (s *Server) sleep(d time.Duration) error {
	select {
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	case <-time.After(d):
		return nil
	}
}

func (s *Server) StartUnix(ctx context.Context, addr *net.UnixAddr) (id int64, status error) {
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		return -1, err
	}
	defer func() {
		if status != nil {
			l.Close()
		}
	}()

	testPath := "/" + uuid.New().String()
	testHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		log.Printf("%s: received test request from %q", addr, r.RemoteAddr)
	})
	s.AddHandler(testPath, testHandler)
	defer s.RemoveHandler(testPath)

	server := &http.Server{
		Handler: s,
		BaseContext: func(net.Listener) context.Context {
			return s.ctx
		},
	}
	defer func() {
		if status != nil {
			server.Close()
		}
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		defer func() {
			if r := recover(); r != nil {
				slog.Error("CAUGHT PANIC", "panic", r)
				slog.Error(string(debug.Stack()))
				panic(r)
			}
		}()

		for s.ctx.Err() == nil {
			if err := server.Serve(l); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					slog.ErrorContext(ctx, "http server failed", "error", err)
				}
			}
		}
	}()

	transport := &http.Transport{
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			return net.DialUnix("unix", nil, addr)
		},
	}
	c := http.Client{
		Timeout:   s.opts.ServerCheckTimeout,
		Transport: transport,
	}
	u := url.URL{
		Scheme: "http",
		Host:   "localhost",
		Path:   testPath,
	}

	tctx, tcancel := context.WithTimeout(ctx, s.opts.ServerCheckTimeout)
	defer tcancel()

	for tctx.Err() == nil {
		r, err := http.NewRequestWithContext(tctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return -1, fmt.Errorf("could not create test request: %w", err)
		}
		resp, err := c.Do(r)
		if err != nil {
			s.sleep(s.opts.ServerCheckRetryInterval)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		break
	}
	if err := context.Cause(tctx); err != nil {
		return -1, fmt.Errorf("could not invoke test handler: %w", err)
	}

	id = s.nextServerID.Add(1) - 1
	s.serverMap.Store(id, server)
	return id, nil
}

func (s *Server) StartTCP(ctx context.Context, addr *net.TCPAddr) (id int64, status error) {
	l, err := net.Listen("tcp", addr.String())
	if err != nil {
		return -1, err
	}
	defer func() {
		if status != nil {
			l.Close()
		}
	}()

	if addr.Port == 0 {
		laddr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			return -1, fmt.Errorf("created listener addr is not *net.TCPAddr type")
		}
		addr.Port = laddr.Port
	}

	testPath := "/" + uuid.New().String()
	testHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		log.Printf("%s: received test request from %q", addr, r.RemoteAddr)
	})
	s.AddHandler(testPath, testHandler)
	defer s.RemoveHandler(testPath)

	server := &http.Server{
		Handler: s,
		BaseContext: func(net.Listener) context.Context {
			return s.ctx
		},
	}
	defer func() {
		if status != nil {
			server.Close()
		}
	}()

	s.wg.Add(1)
	go func() {
		for s.ctx.Err() == nil {
			if err := server.Serve(l); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					slog.ErrorContext(ctx, "http server failed", "error", err)
				}
			}
		}
		s.wg.Done()
	}()

	c := http.Client{
		Timeout: s.opts.ServerCheckTimeout,
	}
	u := url.URL{
		Scheme: "http",
		Host:   l.Addr().String(),
		Path:   testPath,
	}

	tctx, tcancel := context.WithTimeout(ctx, s.opts.ServerCheckTimeout)
	defer tcancel()

	for tctx.Err() == nil {
		r, err := http.NewRequestWithContext(tctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return -1, err
		}
		resp, err := c.Do(r)
		if err != nil {
			s.sleep(s.opts.ServerCheckRetryInterval)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		break
	}

	if err := context.Cause(tctx); err != nil {
		return -1, fmt.Errorf("could not invoke test handler: %w", err)
	}

	id = s.nextServerID.Add(1) - 1
	s.serverMap.Store(id, server)
	return id, nil
}

func (s *Server) Stop(id int64) error {
	svr, ok := s.serverMap.LoadAndDelete(id)
	if !ok {
		return fmt.Errorf("http server %d not found: %w", id, os.ErrNotExist)
	}
	_ = svr.Close()
	return nil
}

func (s *Server) AddHandler(pattern string, handler http.Handler) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.handlerMap[pattern] = handler
	s.updateHandlerMux()
}

func (s *Server) RemoveHandler(pattern string) bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	_, ok := s.handlerMap[pattern]
	if ok {
		return false
	}
	delete(s.handlerMap, pattern)
	s.updateHandlerMux()
	return true
}

func (s *Server) updateHandlerMux() {
	m := http.NewServeMux()
	for k, v := range s.handlerMap {
		m.Handle(k, v)
	}
	s.mux.Store(m)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.Load().ServeHTTP(w, r)
}
