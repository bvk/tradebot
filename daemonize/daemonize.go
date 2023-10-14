// Copyright (c) 2023 BVK Chaitanya

package daemonize

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"log/syslog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// HealthChecker is a function that checks for successfull initialization of
// the background process. It is run only in the parent after respawning itself
// as a child that turns into the background process.
type HealthChecker = func(ctx context.Context, child *os.Process) (retry bool, err error)

// Daemonize uses an environment variable to identify if current process is a
// parent or child process. We expect this environment variable to be unique
// and not used (or set) by any other process. When it's value is non-nil, it
// contains the parent process pid.
var DaemonizeEnvKey = "TRADEBOT_DAEMONIZE"

// Daemonize respawns the current program in the background with the same
// command-line arguments. Daemonize is intended to turn the current program
// invoked from shell into a daemon process. Daemonize function *must* be
// called during the program startup before performing any other significant
// logic, like opening databases, starting servers, etc.
//
// Standard input and standard outputs in the background process are replaced
// with /dev/null and standard library log is redirected to use the syslog
// backend.
//
// Parent process will use the check function to wait for the background
// process to initialize successfully or die unsuccessfully. Check function is
// expected to verify that a new instance of child process is initialized.
//
// When successfull, Daemonize returns nil to the background process and exits
// the parent process (i.e., never returns). When unsuccessful, Daemonize
// returns non-nil error to the parent process and exits the background process
// (i.e., never returns).
func Daemonize(ctx context.Context, check HealthChecker) error {
	if v := os.Getenv(DaemonizeEnvKey); len(v) == 0 {
		if err := daemonizeParent(ctx, check); err != nil {
			return err
		}
		os.Exit(0)
	}
	if err := daemonizeChild(); err != nil {
		os.Exit(1)
	}
	return nil
}

func daemonizeParent(ctx context.Context, check HealthChecker) (status error) {
	binary, err := exec.LookPath(os.Args[0])
	if err != nil {
		return fmt.Errorf("failed to lookup binary: %w", err)
	}
	binaryPath, err := filepath.Abs(binary)
	if err != nil {
		return fmt.Errorf("could not determine absolute path for binary: %w", err)
	}

	file, err := os.OpenFile("/dev/null", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("failed to open /dev/null: %w", err)
	}
	defer file.Close()

	// Receive signal when child-process dies.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGCHLD, os.Interrupt)
	defer stop()

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   []string{fmt.Sprintf("%s=%d", DaemonizeEnvKey, os.Getpid())},
		Files: []*os.File{file, file, file},
	}
	proc, err := os.StartProcess(binaryPath, os.Args, attr)
	if err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}
	defer func() {
		if status != nil {
			if _, err := proc.Wait(); err != nil {
				slog.ErrorContext(ctx, "could not wait for child process cleanup (ignored)", "error", err)
			}
		}
	}()

	if check != nil {
		for sleep := time.Millisecond; ctx.Err() == nil; sleep = sleep << 2 {
			if retry, err := check(ctx, proc); err != nil {
				slog.WarnContext(ctx, "background process is not yet initialized", "error", err)
				if retry {
					time.Sleep(sleep)
					continue
				}
				return fmt.Errorf("background process isn't initialized: %w", err)
			}
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("could not initialize the background process: %w", err)
	}
	return nil
}

func daemonizeChild() error {
	syslogger, err := syslog.New(syslog.LOG_INFO, "tradebot")
	if err != nil {
		return fmt.Errorf("could not create syslog: %w", err)
	}
	log.SetOutput(syslogger)

	if _, err := unix.Setsid(); err != nil {
		return fmt.Errorf("could not set session id: %w", err)
	}
	return nil
}
