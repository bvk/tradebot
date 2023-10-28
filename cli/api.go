// Copyright (c) 2023 BVK Chaitanya

// Package cli implements a minimalistic command-line parsing functionality
// using the standard library's flag.FlagSets.
//
// Users can define commands and group them into subcommands of arbitrary
// depths.
//
// Special top-level commands "help", "flags", and "commands" are added for
// documentation. Documentation is collected through optional interfaces.
//
// # OPTIONAL INTERFACES
//
// Commands can implement `interface{ Synopsis() string }` to provide a short
// one-line description and `interface{ CommandHelp() string }` to provide a
// more detailed multi-line, multi-paragraph documentation.
//
// # EXAMPLE
//
//		type runCmd struct {
//			background  bool
//			port        int
//			ip          string
//			secretsPath string
//			dataDir     string
//		}
//
//		func (r *runCmd) Run(ctx context.Context, args []string) error {
//			if len(p.dataDir) == 0 {
//				p.dataDir = filepath.Join(os.Getenv("HOME"), ".tradebot")
//			}
//			...
//			return nil
//		}
//
//		func (r *runCmd) Command() (*flag.FlagSet, CmdFunc) {
//			fset := flag.NewFlagSet("run", flag.ContinueOnError)
//			f.BoolVar(&p.background, "background", false, "runs the daemon in background")
//			f.IntVar(&p.port, "port", 10000, "TCP port number for the daemon")
//			f.StringVar(&p.ip, "ip", "0.0.0.0", "TCP ip address for the daemon")
//			f.StringVar(&p.secretsPath, "secrets-file", "", "path to credentials file")
//			f.StringVar(&p.dataDir, "data-dir", "", "path to the data directory")
//	    return fset, CmdFunc(r.Run)
//		}
package cli

import (
	"context"
	"flag"
	"os"
)

// CmdFunc defines the signature for command execution functions.
type CmdFunc func(ctx context.Context, args []string) error

// Command interface defines the requirements for Command implementations.
type Command interface {
	// Command function returns the command-line flags and command execution
	// function for a command/subcommand.
	//
	// Command function should return a non-nil flag.FlagSet object with the
	// command name.
	Command() (*flag.FlagSet, CmdFunc)
}

// CommandGroup groups a collection of commands under a parent command. This
// allows for defining subcommands under another command name.
func CommandGroup(name string, cmds ...Command) Command {
	return &cmdGroup{
		flags:   flag.NewFlagSet(name, flag.ContinueOnError),
		subcmds: cmds,
	}
}

// Run parses command-line arguments from `args` into flags and subcommands and
// picks the best command to execute from `cmds`. Top-level command flags from
// flag.CommandLine flags are also processed on the way to resolving the best
// command.
func Run(ctx context.Context, cmds []Command, args []string) error {
	if cmds == nil {
		return os.ErrInvalid
	}
	root := cmdGroup{
		flags:   flag.CommandLine,
		subcmds: cmds,
	}
	return root.run(ctx, args)
}
