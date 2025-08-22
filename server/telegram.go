// Copyright (c) 2025 BVK Chaitanya

package server

import (
	"context"
	"fmt"
	"go/version"
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
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/visvasity/cli"
)

var start = time.Now()

func (s *Server) AddTelegramCommand(ctx context.Context, name, purpose string, handler telegram.CmdFunc) error {
	if s.telegramClient != nil {
		return s.telegramClient.AddCommand(ctx, name, purpose, handler)
	}
	return nil // Ignored
}

func (s *Server) statsCmd(ctx context.Context, _ []string) error {
	p, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	meminfo, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return err
	}
	vminfo, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return err
	}
	hinfo, err := host.InfoWithContext(ctx)
	if err != nil {
		return err
	}
	dinfo, err := disk.UsageWithContext(ctx, s.opts.DataDir)
	if err != nil {
		return err
	}

	durationWithDays := func(d time.Duration) string {
		const day = 24 * time.Hour
		if d < day {
			return fmt.Sprintf("%v", time.Since(start))
		}
		return fmt.Sprintf("%dd%v", int(d/day), d%day)
	}

	stdout := cli.Stdout(ctx)
	fmt.Fprintln(stdout, "System Stats")
	fmt.Fprintf(stdout, "  Hostname: %s\n", hinfo.Hostname)
	fmt.Fprintf(stdout, "  OS: %s\n", hinfo.OS)
	fmt.Fprintf(stdout, "  Platform: %s\n", hinfo.PlatformFamily)
	fmt.Fprintf(stdout, "  OS Version: %s\n", hinfo.PlatformVersion)
	fmt.Fprintf(stdout, "  Kernel Version: %s %s\n", hinfo.KernelVersion, hinfo.KernelArch)
	bootTime := time.Unix(int64(hinfo.BootTime), 0)
	fmt.Fprintf(stdout, "  Uptime: %s\n", durationWithDays(time.Since(bootTime)))
	fmt.Fprintf(stdout, "  Total Memory: %0.2fMB\n", float64(vminfo.Total)/1024/1024)
	fmt.Fprintf(stdout, "  Used Memory: %0.2fMB\n", float64(vminfo.Used)/1024/1024)
	fmt.Fprintf(stdout, "  Free Memory: %0.2fMB\n", float64(vminfo.Free)/1024/1024)
	fmt.Fprintf(stdout, "  Total Storage: %0.2fGB\n", float64(dinfo.Total)/1024/1024/1024)
	fmt.Fprintf(stdout, "  Used Storage: %0.2fGB\n", float64(dinfo.Used)/1024/1024/1024)
	fmt.Fprintf(stdout, "  Free Storage: %0.2fGB\n", float64(dinfo.Free)/1024/1024/1024)
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Service Stats")
	fmt.Fprintf(stdout, "  Binary: %s\n", exe)
	fmt.Fprintf(stdout, "  Data dir: %s\n", s.opts.DataDir)
	fmt.Fprintf(stdout, "  Uptime: %s\n", durationWithDays(time.Since(start)))
	fmt.Fprintf(stdout, "  Virtual Memory: %0.2fMB\n", float64(meminfo.VMS)/1024/1024)
	fmt.Fprintf(stdout, "  Resident Memory: %0.2fMB\n", float64(meminfo.RSS)/1024/1024)
	return nil
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
	slog.Info("found go compiler", "path", goPath)
	// Verify the minimum required go compiler version.
	versionCmd := exec.Command(goPath, "version")
	out, err := versionCmd.Output()
	if err != nil {
		return fmt.Errorf("could not find go compiler version: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 || fields[0] != "go" || fields[1] != "version" {
		return fmt.Errorf("unexpected go version output format")
	}
	slog.Info("found go compiler version as", "version", fields[2])
	if version.Compare(fields[2], "go1.22.0") < 0 {
		return fmt.Errorf("go compiler version %q is too old (want go1.22.0 or higher)", fields[2])
	}

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
