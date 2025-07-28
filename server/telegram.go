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
)

func (s *Server) AddTelegramCommand(ctx context.Context, name, purpose string, handler telegram.CmdFunc) error {
	if s.telegramClient != nil {
		return s.telegramClient.AddCommand(ctx, name, purpose, handler)
	}
	return nil // Ignored
}

func (s *Server) restartCmd(ctx context.Context, args []string) (string, error) {
	if len(args) != 0 {
		return "", fmt.Errorf("too many arguments")
	}
	if len(s.opts.BinaryBackupPath) == 0 {
		return "", fmt.Errorf("binary backup is not found")
	}
	cmd := exec.Command(s.opts.BinaryBackupPath, "run", "-restart")
	if err := cmd.Start(); err != nil {
		return "", err
	}
	return "Restart issued successfully", nil
}

func (s *Server) upgradeCmd(ctx context.Context, args []string) (string, error) {
	target := "latest"
	if len(args) != 0 {
		if len(args) != 1 {
			return "", fmt.Errorf("upgrade command takes at most one argument")
		}
		if strings.ContainsRune(args[0], '@') {
			return "", fmt.Errorf("target branch name %q is invalid", args[0])
		}
		target = args[0]
	}

	binfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "", fmt.Errorf("could not read build info")
	}

	binDir := ""
	if p := os.Getenv("GOPATH"); p != "" {
		binDir = filepath.Join(p, "bin")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not determine user's home directory: %w", err)
		}
		binDir = filepath.Join(homeDir, "go/bin")
	}

	goPath, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go compiler is not found")
	}
	// TODO: Verify the minimum go compiler version.

	installCmd := exec.Command(goPath, "install", binfo.Path+"@"+target)
	if out, err := installCmd.Output(); err != nil {
		return string(out), fmt.Errorf("could not install target version: %w", err)
	}
	binPath := filepath.Join(binDir, "tradebot")
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("could not find installed binary: %w", err)
	}

	runCmd := exec.Command(binPath, "run", "-restart", "-background")
	runCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	slog.Info("restarting with installed version", "target", target, "binPath", binPath)
	if err := runCmd.Start(); err != nil {
		return "", fmt.Errorf("could not start the installed binary: %w", err)
	}
	return fmt.Sprintf("service is upgraded to %q successfully", target), nil
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

func (s *Server) profitTelegramCmd(ctx context.Context, args []string) (string, error) {
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
			return "", err
		}
		var sb strings.Builder
		for i := range keys {
			fmt.Fprintf(&sb, "%s: %s\n", keys[i], vs[i].Profit().StringFixed(3))
		}
		return sb.String(), nil
	}

	switch strings.ToLower(args[0]) {
	case "today":
		vs, err := Summarize(ctx, s.db, timerange.Today(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "yesterday":
		vs, err := Summarize(ctx, s.db, timerange.Yesterday(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "this-week":
		vs, err := Summarize(ctx, s.db, timerange.ThisWeek(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "last-week":
		vs, err := Summarize(ctx, s.db, timerange.LastWeek(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "this-month":
		vs, err := Summarize(ctx, s.db, timerange.ThisMonth(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "last-month":
		vs, err := Summarize(ctx, s.db, timerange.LastMonth(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "this-year":
		vs, err := Summarize(ctx, s.db, timerange.ThisYear(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "last-year":
		vs, err := Summarize(ctx, s.db, timerange.LastYear(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	case "lifetime":
		vs, err := Summarize(ctx, s.db, timerange.Lifetime(time.Local))
		if err != nil {
			return "", err
		}
		return vs[0].Profit().StringFixed(3), nil
	}
	return "", fmt.Errorf("invalid/unsupported arguments")
}
