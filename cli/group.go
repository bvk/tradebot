// Copyright (c) 2023 BVK Chaitanya

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

type cmdGroup struct {
	flags      *flag.FlagSet
	subcmds    []Command
	specialCmd string
}

var specialCmds = []string{"help", "flags", "commands"}

// Command implements Command interface.
func (cg *cmdGroup) Command() (*flag.FlagSet, CmdFunc) {
	return cg.flags, nil
}

func (cg *cmdGroup) printFlags(ctx context.Context, w io.Writer, cmdpath []Command) error {
	fs, _ := cmdpath[len(cmdpath)-1].Command()
	fs.SetOutput(w)
	fs.PrintDefaults()
	return nil
}

func (cg *cmdGroup) printCommands(ctx context.Context, w io.Writer, cmdpath []Command) error {
	subcmds := getSubcommands(cmdpath)
	for _, sub := range subcmds {
		if len(sub[1]) > 0 {
			fmt.Fprintf(w, "\t%-15s  %s\n", sub[0], sub[1])
		} else {
			fmt.Fprintf(w, "\t%-15s\n", sub[0])
		}
	}
	return nil
}

func (cg *cmdGroup) resolve(ctx context.Context, args []string) ([]Command, []string, error) {
	type boolFlag interface {
		flag.Value
		IsBoolFlag() bool
	}

	cmdMap := make(map[string]Command)
	prepCmdMap := func(cmds []Command) {
		m := make(map[string]Command)
		for _, c := range cmds {
			fs, _ := c.Command()
			m[fs.Name()] = c
		}
		cmdMap = m
	}
	prepCmdMap(cg.subcmds)

	fspath := []*flag.FlagSet{flag.CommandLine}
	lookup := func(s string) (*flag.Flag, bool) {
		for i := len(fspath) - 1; i >= 0; i-- {
			fs := fspath[i]
			if f := fs.Lookup(s); f != nil {
				return f, true
			}
		}
		return nil, false
	}

	var i int
	cmdpath := []Command{cg}
	for i = 0; i < len(args); i++ {
		s := args[i]

		// stop resolving subcmds and flags
		if s == "--" {
			i++
			break
		}

		// Non-flag argument
		if len(s) < 2 || s[0] != '-' {
			// non-flag argument to the last subcmd
			if len(cmdMap) == 0 {
				break
			}

			subcmd, ok := cmdMap[s]
			if !ok {
				// handle one of special commands: help, flags, commands
				if len(cmdpath) == 1 && slices.Contains(specialCmds, s) {
					cg.specialCmd = s
					continue
				}
				return nil, nil, fmt.Errorf("command not defined: %s", s)
			}
			cmdpath = append(cmdpath, subcmd)

			// handle subcommands from a command group
			if sg, ok := subcmd.(*cmdGroup); ok {
				prepCmdMap(sg.subcmds)
				continue
			}

			// stop subcommand processing, but continue to resolve flags
			prepCmdMap(nil)
			fs, _ := subcmd.Command()
			fspath = append(fspath, fs)
			continue
		}

		// remove the '-' or '--' prefix and '=...' suffix
		name := s[1:]
		if s[1] == '-' {
			name = s[2:]
		}
		if len(name) == 0 || name[0] == '-' || name[0] == '=' {
			return nil, nil, fmt.Errorf("bad flag syntax: %s", s)
		}
		value := ""
		hasValue := strings.Contains(name, "=")
		if hasValue {
			pos := strings.Index(name, "=")
			name = name[:pos]
			value = name[pos+1:]
		}

		// check for the flag in all the parent FlagSets
		flag, ok := lookup(name)
		if !ok {
			return nil, nil, fmt.Errorf("flag provided but not defined: -%s", name)
		}

		// handle boolean flag, which doesn't need an argument.
		if fv, ok := flag.Value.(boolFlag); ok && fv.IsBoolFlag() {
			if hasValue {
				if err := fv.Set(value); err != nil {
					return nil, nil, fmt.Errorf("invalid boolean value %q for -%s: %w", value, name, err)
				}
			} else {
				if err := fv.Set("true"); err != nil {
					return nil, nil, fmt.Errorf("invalid boolean flag %s: %w", name, err)
				}
			}
			continue
		}

		// non-boolean flags must have a value, which might be the next argument.
		if !hasValue && i+1 < len(args) {
			hasValue = true
			value = args[i+1]
			i++
		}
		if !hasValue {
			return nil, nil, fmt.Errorf("flag needs an argument: -%s", name)
		}
		if err := flag.Value.Set(value); err != nil {
			return nil, nil, fmt.Errorf("invalid value %q for flag -%s: %w", value, name, err)
		}
	}

	return cmdpath, args[i:], nil
}

func (cg *cmdGroup) run(ctx context.Context, args []string) error {
	cmdpath, args, err := cg.resolve(ctx, args)
	if err != nil {
		return err
	}

	switch cg.specialCmd {
	case "help":
		return cg.printHelp(ctx, os.Stderr, cmdpath)
	case "flags":
		return cg.printFlags(ctx, os.Stderr, cmdpath)
	case "commands":
		return cg.printCommands(ctx, os.Stderr, cmdpath)
	}

	_, fun := cmdpath[len(cmdpath)-1].Command()
	if fun == nil {
		return cg.printHelp(ctx, os.Stderr, cmdpath)
	}

	return fun(ctx, args)
}
