// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/daemonize"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
)

type Run struct {
	ServerFlags

	background bool
	noResume   bool

	secretsPath string
	dataDir     string
}

func (c *Run) Synopsis() string {
	return "Runs tradebot in foreground or background"
}

func (c *Run) CommandHelp() string {
	return `

Command "run" starts the tradebot service. Tradebot service scans the database
for existing jobs and resumes them automatically.

SECRETS FILE

Most exchanges require API keys to perform trading operations and
authentication. Users are expected to create a secrets file with API keys in
JSON format. A example secrets file format is given below:

    {
        "coinbase":{
            "key":"111111111",
            "secret":"2222222222"
        }
    }

Users should consult the exchange specific documentation to learn how to create
the API keys.

`
}

func (c *Run) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("run", flag.ContinueOnError)
	c.ServerFlags.SetFlags(fset)
	fset.BoolVar(&c.background, "background", false, "runs the daemon in background")
	fset.BoolVar(&c.noResume, "no-resume", false, "when true old jobs aren't resumed automatically")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	return fset, cli.CmdFunc(c.run)
}

func (c *Run) run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(c.dataDir) == 0 {
		c.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
	}
	if _, err := os.Stat(c.dataDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat data directory %q: %w", c.dataDir, err)
		}
		if err := os.MkdirAll(c.dataDir, 0700); err != nil {
			return fmt.Errorf("could not create data directory %q: %w", c.dataDir, err)
		}
	}

	if len(c.secretsPath) == 0 {
		c.secretsPath = filepath.Join(c.dataDir, "secrets.json")
	}
	secrets, err := trader.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}

	if ip := net.ParseIP(c.ip); ip == nil {
		return fmt.Errorf("invalid ip address")
	}
	if c.port <= 0 {
		return fmt.Errorf("invalid port number")
	}
	addr := &net.TCPAddr{
		IP:   net.ParseIP(c.ip),
		Port: c.port,
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

	if c.background {
		if err := daemonize.Daemonize(ctx, "TRADEBOT_DAEMONIZE", check); err != nil {
			return err
		}
		log.Printf("using data directory %s and secrets file %s", c.dataDir, c.secretsPath)
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
	bopts := badger.DefaultOptions(c.dataDir)
	bdb, err := badger.Open(bopts)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer bdb.Close()
	db := kvbadger.New(bdb, isGoodKey)

	s.AddHandler("/db/", http.StripPrefix("/db", kvhttp.Handler(db)))

	// Start other services.
	topts := &trader.Options{
		NoResume: c.noResume,
	}
	trader, err := trader.NewTrader(secrets, db, topts)
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

	if err := trader.Start(ctx); err != nil {
		return err
	}
	defer func() {
		if err := trader.Stop(context.Background()); err != nil {
			log.Printf("could not stop all jobs (ignored): %v", err)
		}
	}()

	// Wait for the signals

	log.Printf("started tradebot server at %v:%d", opts.ListenIP, opts.ListenPort)
	s.AddHandler("/pid", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, fmt.Sprintf("%d", os.Getpid()))
	}))

	<-ctx.Done()
	log.Printf("tradebot server is shutting down")
	return nil
}

func isGoodKey(k string) bool {
	return path.IsAbs(k) && k == path.Clean(k)
}
