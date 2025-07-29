// Copyright (c) 2025 BVK Chaitanya

package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/bvk/tradebot/telegram"
	"github.com/bvk/tradebot/timerange"
	"github.com/bvk/tradebot/trader"
	"github.com/bvkgo/kv"
	"github.com/visvasity/cli"
)

func (s *Server) AddTelegramCommand(ctx context.Context, name, purpose string, handler telegram.CmdFunc) error {
	if s.telegramClient != nil {
		return s.telegramClient.AddCommand(ctx, name, purpose, handler)
	}
	return nil // Ignored
}

func (s *Server) restartCmd(ctx context.Context, args []string) error {
	stdout := cli.Stdout(ctx)
	if len(args) != 0 {
		return fmt.Errorf("too many arguments")
	}
	if len(s.opts.BinaryBackupPath) == 0 {
		return fmt.Errorf("binary backup is not found")
	}
	cmd := exec.Command(s.opts.BinaryBackupPath, "run", "-restart")
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Restart issued successfully")
	return nil
}

func (s *Server) upgradeCmd(ctx context.Context, args []string) error {
	stdout := cli.Stdout(ctx)
	target := "latest"
	if len(args) != 0 {
		if len(args) != 1 {
			return fmt.Errorf("upgrade command takes at most one argument")
		}
		if strings.ContainsRune(args[0], '@') {
			return fmt.Errorf("target branch name %q is invalid", args[0])
		}
		target = args[0]
	}

	binfo, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("could not read build info")
	}

	binDir := ""
	if p := os.Getenv("GOPATH"); p != "" {
		binDir = filepath.Join(p, "bin")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("could not determine user's home directory: %w", err)
		}
		binDir = filepath.Join(homeDir, "go/bin")
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go compiler is not found")
	}
	// TODO: Verify the minimum go compiler version.

	installCmd := exec.Command(goPath, "install", binfo.Path+"@"+target)
	if _, err := installCmd.Output(); err != nil {
		return fmt.Errorf("could not install target version: %w", err)
	}
	binPath := filepath.Join(binDir, "tradebot")
	if _, err := os.Stat(binPath); err != nil {
		return fmt.Errorf("could not find installed binary: %w", err)
	}

	runCmd := exec.Command(binPath, "run", "-restart", "-background")
	runCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	slog.Info("restarting with installed version", "target", target, "binPath", binPath)
	if err := runCmd.Start(); err != nil {
		return fmt.Errorf("could not start the installed binary: %w", err)
	}
	go func() {
		if err := runCmd.Wait(); err != nil {
			slog.Error("installed binary has failed to initialize", "err", err)
			s.telegramClient.SendMessage(ctx, time.Now(), "installed binary has failed to initialize.")
		}
	}()
	fmt.Fprintln(stdout, "installed binary is started in the background.")
	return nil
}

func Summarize(ctx context.Context, db kv.Database, periods ...*timerange.Range) ([]*trader.Summary, error) {
	var traders []trader.Trader
	loadf := func(ctx context.Context, r kv.Reader) error {
		vs, err := LoadAll(ctx, r)
		if err != nil {
			return err
		}
		traders = vs
		return nil
	}
	if err := kv.WithReader(ctx, db, loadf); err != nil {
		return nil, err
	}

	var summaries []*trader.Summary
	for _, period := range periods {
		var statuses []*trader.Status
		for _, t := range traders {
			if x, ok := t.(trader.Statuser); ok {
				statuses = append(statuses, x.Status(period))
			}
		}
		summaries = append(summaries, trader.Summarize(statuses))
	}
	return summaries, nil
}

func (s *Server) profitTelegramCmd(ctx context.Context, args []string) error {
	stdout := cli.Stdout(ctx)
	if len(args) == 0 {
		ps := []*timerange.Range{
			timerange.Today(time.Local),
			timerange.Yesterday(time.Local),
			timerange.ThisWeek(time.Local),
			timerange.LastWeek(time.Local),
			timerange.ThisMonth(time.Local),
			timerange.LastMonth(time.Local),
			timerange.ThisYear(time.Local),
			timerange.LastYear(time.Local),
			timerange.Lifetime(time.Local),
		}
		keys := []string{
			"Today",
			"Yesterday",
			"This Week",
			"Last Week",
			"This Month",
			"Last Month",
			"This Year",
			"Last Year",
			"Lifetime",
		}
		vs, err := Summarize(ctx, s.db, ps...)
		if err != nil {
			return err
		}
		for i := range keys {
			fmt.Fprintf(stdout, "%s: %s\n", keys[i], vs[i].Profit().StringFixed(3))
		}
		return nil
	}

	var err error
	var summaries []*trader.Summary
	switch strings.ToLower(args[0]) {
	case "today":
		summaries, err = Summarize(ctx, s.db, timerange.Today(time.Local))
	case "yesterday":
		summaries, err = Summarize(ctx, s.db, timerange.Yesterday(time.Local))
	case "this-week":
		summaries, err = Summarize(ctx, s.db, timerange.ThisWeek(time.Local))
	case "last-week":
		summaries, err = Summarize(ctx, s.db, timerange.LastWeek(time.Local))
	case "this-month":
		summaries, err = Summarize(ctx, s.db, timerange.ThisMonth(time.Local))
	case "last-month":
		summaries, err = Summarize(ctx, s.db, timerange.LastMonth(time.Local))
	case "this-year":
		summaries, err = Summarize(ctx, s.db, timerange.ThisYear(time.Local))
	case "last-year":
		summaries, err = Summarize(ctx, s.db, timerange.LastYear(time.Local))
	case "lifetime":
		summaries, err = Summarize(ctx, s.db, timerange.Lifetime(time.Local))
	default:
		return fmt.Errorf("invalid/unsupported arguments")
	}
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, summaries[0].Profit().StringFixed(3))
	return nil
}
