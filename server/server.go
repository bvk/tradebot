// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Server struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup

	opts Options

	server *http.Server
	mux    atomic.Pointer[http.ServeMux]

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

	addr := &net.TCPAddr{
		IP:   opts.ListenIP,
		Port: opts.ListenPort,
	}
	if err := s.start(ctx, addr); err != nil {
		return nil, err
	}
	if opts.ListenPort == 0 {
		opts.ListenPort = addr.Port
		s.opts.ListenPort = addr.Port
	}
	return s, nil
}

func (s *Server) Close() error {
	// TODO: shutdown the server.
	s.cancel(os.ErrClosed)
	s.stop()
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

func (s *Server) start(ctx context.Context, addr *net.TCPAddr) (status error) {
	l, err := net.Listen("tcp", addr.String())
	if err != nil {
		return err
	}
	defer func() {
		if status != nil {
			l.Close()
		}
	}()

	if addr.Port == 0 {
		laddr, ok := l.Addr().(*net.TCPAddr)
		if !ok {
			return fmt.Errorf("created listener addr is not *net.TCPAddr type")
		}
		addr.Port = laddr.Port
	}

	testPath := "/" + uuid.New().String()
	testHandler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
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

	for ctx.Err() == nil {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return err
		}
		if _, err := c.Do(r); err != nil {
			s.sleep(s.opts.ServerCheckRetryInterval)
		}
		break
	}

	s.server = server
	return nil
}

func (s *Server) stop() error {
	if s.server == nil {
		return os.ErrInvalid
	}
	_ = s.server.Close()
	s.server = nil
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
