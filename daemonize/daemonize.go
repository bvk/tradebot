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
// expected to verify that a new instance of child process is initialized
// successfully.
//
// When successful, Daemonize returns nil to the background process and exits
// the parent process (i.e., never returns). When unsuccessful, Daemonize
// returns non-nil error to the parent process and exits the background process
// (i.e., never returns).
func Daemonize(ctx context.Context, check func(context.Context) error) error {
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

func daemonizeParent(ctx context.Context, check func(context.Context) error) error {
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

	// Receive signal when child-process dies.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGCHLD, os.Interrupt)
	defer stop()

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   []string{fmt.Sprintf("%s=%d", DaemonizeEnvKey, os.Getpid())},
		Files: []*os.File{file, file, file},
	}
	if _, err := os.StartProcess(binaryPath, os.Args, attr); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	if check != nil {
		time.Sleep(time.Second)
		for ctx.Err() == nil {
			if err := check(ctx); err != nil {
				slog.WarnContext(ctx, "daemon process not yet initialized", "error", err)
				time.Sleep(time.Second)
				continue
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
