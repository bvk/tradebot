// Copyright (c) 2026 BVK Chaitanya

package xvnc

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/KarpelesLab/vncpasswd"
)

type Server struct {
	lifeCtx    context.Context
	lifeCancel context.CancelCauseFunc

	wg sync.WaitGroup

	display int
	dataDir string

	cmd atomic.Pointer[exec.Cmd]
}

func NewServer(dataDir string, display int) (*Server, error) {
	if _, err := exec.LookPath("Xvnc"); err != nil {
		return nil, fmt.Errorf("Xvnc command not found")
	}

	lifeCtx, lifeCancel := context.WithCancelCause(context.Background())
	s := &Server{
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,

		dataDir: dataDir,
		display: display,
	}

	passwdFile := s.PasswordFile()
	encrypted := vncpasswd.Crypt(randPassword())
	if err := os.WriteFile(passwdFile, encrypted, os.FileMode(0600)); err != nil {
		slog.Error("could not create vnc password file", "err", err)
		return nil, err
	}

	return s, nil
}

func (s *Server) Close() error {
	s.lifeCancel(os.ErrClosed)
	if c := s.cmd.Load(); c != nil {
		c.Process.Kill()
	}
	s.wg.Wait()
	return nil
}

func (s *Server) RunWithDisplay(cmd *exec.Cmd) error {
	cmd.Env = append(cmd.Env, fmt.Sprintf("DISPLAY=:%d", s.display))
	return cmd.Run()
}

func (s *Server) PasswordFile() string {
	return filepath.Join(s.dataDir, "vncpasswd")
}

func (s *Server) ServerSocket() string {
	return filepath.Join(s.dataDir, "vncserver.sock")
}

func (s *Server) Start(addr *net.TCPAddr) (status error) {
	if c := s.cmd.Load(); c != nil {
		return fmt.Errorf("server is already running")
	}

	if addr == nil {
		addr = &net.TCPAddr{
			IP:   net.IPv4(0, 0, 0, 0),
			Port: 5900 + s.display,
		}
	}

	var environ []string
	environ = append(environ, "HOME="+os.Getenv("HOME"))
	environ = append(environ, "PATH="+os.Getenv("PATH"))

	vncDisplay := fmt.Sprintf(":%d", s.display)
	passwdFile := s.PasswordFile()
	vncCmdline := []string{
		"Xvnc",
		"-listen", addr.IP.String(),
		"-rfbport", strconv.Itoa(addr.Port),
		"-rfbauth", passwdFile,
		vncDisplay,
	}

	cmd := exec.CommandContext(s.lifeCtx, vncCmdline[0])
	if !s.cmd.CompareAndSwap(nil, cmd) {
		return fmt.Errorf("lost the race against another run call")
	}
	defer func() {
		if status != nil {
			s.cmd.CompareAndSwap(cmd, nil)
		}
	}()

	cmd.Args = vncCmdline
	cmd.Env = environ
	stderr, err := cmd.StderrPipe()
	if err != nil {
		slog.Error("could not create stderr pipe", "err", err)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		slog.Error("could not create stdout pipe", "err", err)
		return err
	}

	slog.Info("running Xvnc server", "cmdline", cmd.Args)
	if err := cmd.Start(); err != nil {
		slog.Error("could not start Xvnc server instance", "err", err)
		return err
	}

	s.wg.Add(1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			slog.Info(scanner.Text())
		}
		s.cmd.CompareAndSwap(cmd, nil)
		s.wg.Done()
	}()

	s.wg.Add(1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Split(bufio.ScanLines)
		for scanner.Scan() {
			slog.Info(scanner.Text())
		}
		s.cmd.CompareAndSwap(cmd, nil)
		s.wg.Done()
	}()

	// // Connect a dummy vnc client to the session so that everything is
	// // initialized.
	// s.wg.Add(1)
	// go func() {
	// 	ctxutil.Sleep(s.lifeCtx, time.Second)
	// 	ctx, cancel := context.WithTimeout(s.lifeCtx, time.Second)
	// 	defer cancel()
	// 	cmd := exec.CommandContext(ctx, "vncviewer", "-passwd", passwdFile, viewerPath)
	// 	s.RunWithDisplay(cmd)
	// 	s.wg.Done()
	// }()

	slog.Info("started Xvnc server successfully", "DISPLAY", vncDisplay)
	return nil
}

func (s *Server) Run(ctx context.Context, addr *net.TCPAddr) error {
	if err := s.Start(addr); err != nil {
		return err
	}

	c := s.cmd.Load()
	if c == nil {
		return nil
	}

	stopf := context.AfterFunc(ctx, func() {
		c.Process.Kill()
	})
	defer stopf()

	s.wg.Wait()
	return c.Wait()
}

func randPassword() string {
	bs := make([]byte, 8)
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i := 0; i < len(bs); i++ {
		bs[i] = chars[rand.Intn(len(bs))]
	}
	return string(bs)
}
