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
	"net/http/pprof"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bvk/tradebot/cli"
	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/daemonize"
	"github.com/bvk/tradebot/httputil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
	"github.com/nightlyone/lockfile"
)

type Run struct {
	cmdutil.ServerFlags

	background bool

	restart         bool
	shutdownTimeout time.Duration

	noPprof        bool
	noResume       bool
	noFetchCandles bool

	secretsPath string
	dataDir     string
}

func (c *Run) Command() (*flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("run", flag.ContinueOnError)
	c.ServerFlags.SetFlags(fset)
	fset.BoolVar(&c.background, "background", false, "runs the daemon in background")
	fset.BoolVar(&c.restart, "restart", false, "when true, kills any old instance")
	fset.DurationVar(&c.shutdownTimeout, "shutdown-timeout", 30*time.Second, "max timeout for shutdown when restarting")
	fset.BoolVar(&c.noPprof, "no-pprof", false, "when true net/http/pprof handler is not registered")
	fset.BoolVar(&c.noResume, "no-resume", false, "when true old jobs aren't resumed automatically")
	fset.BoolVar(&c.noFetchCandles, "no-fetch-candles", false, "when true, candle data is not saved in the datastore")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.dataDir, "data-dir", "", "path to the data directory")
	return fset, cli.CmdFunc(c.run)
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
	dataDir, err := filepath.Abs(c.dataDir)
	if err != nil {
		return fmt.Errorf("could not determine data-dir %q absolute path: %w", c.dataDir, err)
	}

	if len(c.secretsPath) == 0 {
		c.secretsPath = filepath.Join(dataDir, "secrets.json")
	}
	secrets, err := server.SecretsFromFile(c.secretsPath)
	if err != nil {
		return err
	}

	if ip := net.ParseIP(c.IP); ip == nil {
		return fmt.Errorf("invalid ip address")
	}
	if c.Port <= 0 {
		return fmt.Errorf("invalid port number")
	}
	addr := &net.TCPAddr{
		IP:   net.ParseIP(c.IP),
		Port: c.Port,
	}

	// Health checker for the background process initialization. We need to
	// verify that responding http server is really our child and not an older
	// instance.
	check := func(ctx context.Context, child *os.Process) (bool, error) {
		client := http.Client{Timeout: time.Second}
		resp, err := client.Get(fmt.Sprintf("http://%s/pid", addr.String()))
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
			if !c.restart {
				return false, fmt.Errorf("is another instance already running? pid mismatch: want %d got %s", child.Pid, pid)
			}
			return true, fmt.Errorf("is another instance already running? pid mismatch: want %d got %s", child.Pid, pid)
		}
		return false, nil
	}

	if c.background {
		if err := daemonize.Daemonize(ctx, "TRADEBOT_DAEMONIZE", check); err != nil {
			return err
		}
	}

	log.SetFlags(log.Flags() | log.Lmicroseconds)
	log.Printf("using data directory %s and secrets file %s", dataDir, c.secretsPath)

	lockPath := filepath.Join(dataDir, "tradebot.lock")
	flock, err := lockfile.New(lockPath)
	if err != nil {
		return fmt.Errorf("could not create lock file %q: %w", lockPath, err)
	}
	if err := flock.TryLock(); err != nil {
		if !c.restart {
			return fmt.Errorf("could not get lock on file %q: %w", lockPath, err)
		}
		owner, err := flock.GetOwner()
		if err != nil {
			return fmt.Errorf("could not get current owner of the lock file: %w", err)
		}
		if err := owner.Signal(os.Interrupt); err == nil {
			log.Printf("waiting for the previous instance to shutdown")
			if err := ctxutil.RetryTimeout(ctx, time.Second, c.shutdownTimeout, flock.TryLock); err != nil {
				if err := owner.Signal(os.Kill); err != nil {
					return fmt.Errorf("could not kill current owner of the lock file: %w", err)
				}
				ctxutil.Sleep(ctx, time.Millisecond)
			}
		}
		if err := flock.TryLock(); err != nil {
			return fmt.Errorf("could not get lock on file %q after killing previous instance: %w", lockPath, err)
		}
	}
	defer flock.Unlock()

	// Start HTTP server.
	s, err := httputil.New(nil /* opts */)
	if err != nil {
		return err
	}
	defer s.Close()

	tcpServer, err := s.StartTCP(ctx, addr)
	if err != nil {
		return fmt.Errorf("could not start http server on %s: %w", addr, err)
	}
	defer s.Stop(tcpServer)

	if !c.noPprof {
		s.AddHandler("/debug/pprof/heap", pprof.Handler("heap"))
		s.AddHandler("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		s.AddHandler("/debug/pprof/allocs", pprof.Handler("allocs"))
		s.AddHandler("/debug/pprof/block", pprof.Handler("block"))
		s.AddHandler("/debug/pprof/mutex", pprof.Handler("mutex"))
	}

	// Open the database.
	bopts := badger.DefaultOptions(dataDir)
	bdb, err := badger.Open(bopts)
	if err != nil {
		return fmt.Errorf("could not open the database: %w", err)
	}
	defer bdb.Close()
	db := kvbadger.New(bdb, isGoodKey)

	s.AddHandler("/db/", http.StripPrefix("/db", kvhttp.Handler(db)))

	// Start other services.
	topts := &server.Options{
		NoResume:       c.noResume,
		NoFetchCandles: c.noFetchCandles,
	}
	trader, err := server.New(secrets, db, topts)
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

	log.Printf("started tradebot server at %s", addr)
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
