// Copyright (c) 2023 BVK Chaitanya

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"time"

	"github.com/bvkgo/kvbadger"
	"github.com/bvkgo/tradebot/daemonize"
	"github.com/bvkgo/tradebot/server"
	"github.com/bvkgo/tradebot/trader"
	"github.com/dgraph-io/badger/v4"
	"github.com/google/subcommands"
)

type runCmd struct {
	background  bool
	port        int
	ip          string
	secretsPath string
	dataDir     string
}

func (*runCmd) Name() string     { return "run" }
func (*runCmd) Synopsis() string { return "runs the trading bot daemon" }
func (*runCmd) Usage() string {
	return `run [options]:
  Runs the trading bot daemon.
`
}

func (p *runCmd) SetFlags(f *flag.FlagSet) {
	f.BoolVar(&p.background, "background", false, "runs the daemon in background")
	f.IntVar(&p.port, "port", 10000, "TCP port number for the daemon")
	f.StringVar(&p.ip, "ip", "0.0.0.0", "TCP ip address for the daemon")
	f.StringVar(&p.secretsPath, "secrets-file", "", "path to credentials file")
	f.StringVar(&p.dataDir, "data-dir", "", "path to the data directory")
}

func (p *runCmd) Execute(ctx context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	if err := p.run(ctx, f); err != nil {
		slog.ErrorContext(ctx, "run:", "error", err)
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

func (p *runCmd) run(ctx context.Context, f *flag.FlagSet) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if len(p.dataDir) == 0 {
		p.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(p.dataDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat data directory %q: %w", p.dataDir, err)
		}
		if err := os.MkdirAll(p.dataDir, 0700); err != nil {
			return fmt.Errorf("could not create data directory %q: %w", p.dataDir, err)
		}
	}

	if len(p.secretsPath) == 0 {
		p.secretsPath = filepath.Join(p.dataDir, "secrets.json")
	}
	secrets, err := trader.SecretsFromFile(p.secretsPath)
	if err != nil {
		return err
	}

	if ip := net.ParseIP(p.ip); ip == nil {
		return fmt.Errorf("invalid ip address")
	}
	if p.port <= 0 {
		return fmt.Errorf("invalid port number")
	}
	addr := &net.TCPAddr{
		IP:   net.ParseIP(p.ip),
		Port: p.port,
	}

	// Health checker for the background process initialization. We need to
	// verify that responding http server is really our child and not an older
	// instance.
	check := func(ctx context.Context, child *os.Process) (bool, error) {
		c := http.Client{Timeout: time.Second}
		resp, err := c.Get(fmt.Sprintf("http://%s/pid", addr.String()))
		if err != nil {
			return true, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return true, fmt.Errorf("http status: %d", resp.StatusCode)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return true, err
		}
		if pid := string(data); pid != fmt.Sprintf("%d", child.Pid) {
			return false, fmt.Errorf("is another instance already running? pid mismatch: want %d got %s", child.Pid, pid)
		}
		return false, nil
	}

	if p.background {
		if err := daemonize.Daemonize(ctx, "TRADEBOT_DAEMONIZE", check); err != nil {
			return err
		}
	}

	// Start HTTP server.
	opts := &server.Options{
		ListenIP:   addr.IP,
		ListenPort: addr.Port,
	}
	s, err := server.New(opts)
	if err != nil {
		return err
	}
	defer s.Close()

	// Open the database.
	bopts := badger.DefaultOptions(p.dataDir)
	bdb, err := badger.Open(bopts)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer bdb.Close()
	db := kvbadger.New(bdb, isGoodKey)

	// Start other services.
	trader, err := trader.NewTrader(secrets, db)
	if err != nil {
		return err
	}
	defer trader.Close()

	// Add trader api handlers
	traderAPIs := trader.HandlerMap()
	for k, v := range traderAPIs {
		s.AddHandler(k, v)
	}
	defer func() {
		for k := range traderAPIs {
			s.RemoveHandler(k)
		}
	}()

	slog.InfoContext(ctx, "started tradebot server", "ip", opts.ListenIP, "port", opts.ListenPort)
	s.AddHandler("/pid", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, fmt.Sprintf("%d", os.Getpid()))
	}))
	<-ctx.Done()
	slog.InfoContext(ctx, "tradebot server is shutting down")
	return nil
}

func isGoodKey(k string) bool {
	return path.IsAbs(k) && k == path.Clean(k)
}
