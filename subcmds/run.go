// Copyright (c) 2023 BVK Chaitanya

package subcmds

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bvk/tradebot/ctxutil"
	"github.com/bvk/tradebot/daemonize"
	"github.com/bvk/tradebot/httputil"
	"github.com/bvk/tradebot/server"
	"github.com/bvk/tradebot/subcmds/cmdutil"
	"github.com/bvk/tradebot/subcmds/defaults"
	"github.com/bvkgo/kv/kvhttp"
	"github.com/bvkgo/kvbadger"
	"github.com/dgraph-io/badger/v4"
	"github.com/nightlyone/lockfile"
	"github.com/visvasity/cli"
	"github.com/visvasity/sglog"
)

type Run struct {
	cmdutil.ServerFlags

	background bool

	waitforHostPort string

	restart         bool
	shutdownTimeout time.Duration

	logDir   string
	logDebug bool

	noPprof              bool
	noResume             bool
	noFetchCandles       bool
	maxFetchTimeLatency  time.Duration
	maxHttpClientTimeout time.Duration

	secretsPath string
	dataDir     string
}

func (c *Run) Command() (string, *flag.FlagSet, cli.CmdFunc) {
	fset := flag.NewFlagSet("run", flag.ContinueOnError)
	c.ServerFlags.SetFlags(fset)
	fset.BoolVar(&c.background, "background", false, "runs the daemon in background")
	fset.BoolVar(&c.restart, "restart", false, "when true, kills any old instance")
	fset.DurationVar(&c.shutdownTimeout, "shutdown-timeout", 30*time.Second, "max timeout for shutdown when restarting")
	fset.BoolVar(&c.logDebug, "log-debug", false, "when true, debug messages are logged")
	fset.BoolVar(&c.noPprof, "no-pprof", false, "when true net/http/pprof handler is not registered")
	fset.BoolVar(&c.noResume, "no-resume", false, "when true old jobs aren't resumed automatically")
	fset.BoolVar(&c.noFetchCandles, "no-fetch-candles", true, "when true, candle data is not saved in the datastore")
	fset.DurationVar(&c.maxFetchTimeLatency, "max-fetch-time-latency", 0, "max latency for fetch-time operation in finding time difference")
	fset.DurationVar(&c.maxHttpClientTimeout, "max-http-client-timeout", 30*time.Second, "default max timeout for http requests")
	fset.StringVar(&c.secretsPath, "secrets-file", "", "path to credentials file")
	fset.StringVar(&c.dataDir, "data-dir", defaults.DataDir(), "path to the data directory")
	fset.StringVar(&c.logDir, "log-dir", defaults.LogDir(), "path to the logs directory")
	fset.StringVar(&c.waitforHostPort, "waitfor-host-port", "", "startup waits till host:port becomes reachable")
	return "run", fset, cli.CmdFunc(c.run)
}

func (c *Run) Purpose() string {
	return "Runs tradebot in foreground or background"
}

func (c *Run) Description() string {
	return `

Command "run" starts the tradebot service. Tradebot service scans the database
for existing jobs and resumes them automatically.

SECRETS FILE

Most exchanges require API keys to perform trading operations and
authentication. Users are expected to create a secrets file with API keys in
JSON format. A example secrets file format is given below:

    {
        "coinbase":{
            "kid":"organizations/org-uuid/apiKeys/key-uuid",
            "pem":"-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIBXCw....7smegg==\n-----END EC PRIVATE KEY-----\n"
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
	if len(c.logDir) == 0 {
		return fmt.Errorf("logs directory cannot be empty: %w", os.ErrInvalid)
	}
	if _, err := os.Stat(c.logDir); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("could not stat log directory %q: %w", c.logDir, err)
		}
		if err := os.MkdirAll(c.logDir, 0700); err != nil {
			return fmt.Errorf("could not create log directory %q: %w", c.logDir, err)
		}
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
	if c.Port() <= 0 {
		return fmt.Errorf("invalid port number")
	}
	addr := &net.TCPAddr{
		IP:   net.ParseIP(c.IP),
		Port: c.Port(),
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

	if err := c.waitforGlobalUnicastIP(ctx); err != nil {
		return err
	}
	if len(c.waitforHostPort) > 0 {
		if err := c.waitforDial(ctx, c.waitforHostPort); err != nil {
			return err
		}
	}
	if err := c.waitforDial(ctx, "api.coinex.com:443"); err != nil {
		return err
	}
	if err := c.waitforDial(ctx, "api.coinbase.com:443"); err != nil {
		return err
	}

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

	// Make a copy of the binary into the data directory, so that /restart
	// telegram command can guarantee to restart the same version.
	if err := c.backupBinary(ctx); err != nil {
		slog.Error("could not make a backup copy of the binary", "err", err)
		return err
	}

	log.SetFlags(log.Lshortfile)
	backend := sglog.NewBackend(&sglog.Options{
		LogFileHeader:        true,
		LogDirs:              []string{c.logDir},
		LogFileMaxSize:       100 * 1024 * 1024,
		LogFileReuseDuration: time.Hour,
	})
	defer backend.Close()

	slog.SetDefault(slog.New(backend.Handler()))
	log.Printf("using data directory %s, log directory %s and secrets file %s", dataDir, c.logDir, c.secretsPath)

	if c.logDebug {
		backend.SetLevel(slog.LevelDebug)
	}

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
		s.AddHandler("/debug/pprof/", http.HandlerFunc(pprof.Index))
		s.AddHandler("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
		s.AddHandler("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
		s.AddHandler("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
		s.AddHandler("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	}
	s.AddHandler("/debug/logging/on", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		backend.SetLevel(slog.LevelDebug)
		slog.Info("debug logging is turned on by the user through REST endpoint")
	}))
	s.AddHandler("/debug/logging/off", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		slog.Info("debug logging is turned off by the user through REST endpoint")
		backend.SetLevel(slog.LevelInfo)
	}))

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
		NoResume:             c.noResume,
		NoFetchCandles:       c.noFetchCandles,
		MaxFetchTimeLatency:  c.maxFetchTimeLatency,
		MaxHttpClientTimeout: c.maxHttpClientTimeout,
	}
	trader, err := server.New(ctx, secrets, db, topts)
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

func (c *Run) backupBinary(ctx context.Context) (status error) {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	sfi, err := os.Stat(src)
	if err != nil {
		return err
	}
	dst := filepath.Join(c.dataDir, "tradebot")
	dfi, err := os.Stat(dst)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err == nil {
		if mode := dfi.Mode(); !mode.IsRegular() {
			return fmt.Errorf("binary target path %q is not a regular file", dst)
		}
		if os.SameFile(sfi, dfi) {
			return nil
		}
	}
	if err = os.Link(src, dst); err == nil {
		return nil
	}

	sfp, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sfp.Close()

	dfp, err := os.CreateTemp(c.dataDir, "tradebot*****")
	if err != nil {
		return err
	}
	defer dfp.Close()

	tmpName := dfp.Name()
	defer func() {
		if status != nil {
			os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(dfp, sfp); err != nil {
		return err
	}
	if err := dfp.Sync(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, os.FileMode(0755)); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	return nil
}

func (c *Run) waitforDial(ctx context.Context, hostport string) error {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return fmt.Errorf("invalid wait-for-hostport value: %w", err)
	}
	addr := net.JoinHostPort(host, port)

	for ctx.Err() == nil {
		dctx, dcancel := context.WithTimeout(ctx, time.Second)
		var d net.Dialer
		conn, err := d.DialContext(dctx, "tcp", addr)
		dcancel()

		if err == nil {
			slog.Info("connect to target is successful", "target", addr)
			conn.Close()
			return nil
		}

		slog.Info("sleeping to retry checking for target is reachable", "target", addr)
		ctxutil.Sleep(ctx, time.Second)
	}
	return context.Cause(ctx)
}

func (c *Run) waitforGlobalUnicastIP(ctx context.Context) error {
	for ctx.Err() == nil {
		ifaces, err := net.Interfaces()
		if err != nil {
			return err
		}
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 {
				slog.Debug("interface is skipped cause it is down", "iface", iface.Name, "flags", iface.Flags)
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				return err
			}
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err != nil {
					continue
				}
				if ip.IsLoopback() {
					slog.Debug("loopback ip is skipped", "iface", iface.Name, "addr", addr)
					continue
				}
				if !ip.IsGlobalUnicast() {
					slog.Debug("non-global-unicast ip is skipped", "iface", iface.Name, "addr", addr)
					continue
				}
				if !ip.Equal(ip.To4()) {
					slog.Debug("non-ip-v4 ip is skipped", "iface", iface.Name, "addr", addr)
					continue
				}
				slog.Info("at least one global unicast ip is found", "iface", iface.Name, "ip", ip)
				return nil
			}
		}
		slog.Info("sleeping to retry checking for global unicast ip")
		ctxutil.Sleep(ctx, time.Second)
	}
	return context.Cause(ctx)
}
