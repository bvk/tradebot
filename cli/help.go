// Copyright (c) 2023 BVK Chaitanya

package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

func numFlags(fs *flag.FlagSet) int {
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	return n
}

func getName(c Command) string {
	fs, _ := c.Command()
	_, file := filepath.Split(fs.Name())
	return file
}

func getUsage(cmdpath []Command) string {
	var words []string

	for i, c := range cmdpath {
		fs, _ := c.Command()
		name := fs.Name()
		if i == 0 {
			_, name = filepath.Split(fs.Name())
		}
		words = append(words, name)
	}

	for _, c := range cmdpath {
		fs, _ := c.Command()
		if n := numFlags(fs); n != 0 {
			words = append(words, "<flags>")
			break
		}
	}

	if _, ok := cmdpath[len(cmdpath)-1].(*cmdGroup); ok {
		words = append(words, "<subcommand>")
	}

	words = append(words, "<args>")
	return strings.Join(words, " ")
}

func getHelpDoc(c Command) string {
	if v, ok := c.(interface{ CommandHelp() string }); ok {
		return v.CommandHelp()
	}
	return getSynopsis(c)
}

func getSynopsis(c Command) string {
	if v, ok := c.(interface{ Synopsis() string }); ok {
		return v.Synopsis()
	}
	if v, ok := c.(*cmdGroup); ok {
		return v.synopsis
	}
	return ""
}

func getFlags(c Command) (*flag.FlagSet, int) {
	fs, _ := c.Command()
	return fs, numFlags(fs)
}

func getInheritedFlags(cmdpath []Command) (*flag.FlagSet, int) {
	flagMap := make(map[string][]*flag.Flag)
	collector := func(f *flag.Flag) {
		fs := flagMap[f.Name]
		flagMap[f.Name] = append(fs, f)
	}
	// Collect flag.Flag values defined by ancestors from the command path. A
	// flag may be defined multiple times unfortunately, in which case, we pick
	// the closest/deepest flag.Flag to the currently running command.
	for i := 0; i < len(cmdpath)-1; i++ {
		fs, _ := cmdpath[i].Command()
		fs.VisitAll(collector)
	}
	fset := flag.NewFlagSet("temp", flag.ContinueOnError)
	for _, fs := range flagMap {
		last := fs[len(fs)-1]
		fset.Var(last.Value, last.Name, last.Usage)
	}
	return fset, numFlags(fset)
}

// getSubcommands returns all subcommand names and synopsises as a pair.
func getSubcommands(cmdpath []Command) [][2]string {
	var result [][2]string
	if len(cmdpath) == 1 {
		result = [][2]string{
			[2]string{"help", "describe subcommands and flags"},
			[2]string{"flags", "describe all known flags"},
			[2]string{"commands", "list all command names"},
			[2]string{},
		}
	}

	var subcmds [][2]string
	if cg, ok := cmdpath[len(cmdpath)-1].(*cmdGroup); ok {
		for _, c := range cg.subcmds {
			n, s := getName(c), getSynopsis(c)
			subcmds = append(subcmds, [2]string{n, s})
		}
	}

	// Sort subcommands such that items with no synopsis come first and also
	// group sorted by name.
	sort.Slice(subcmds, func(i, j int) bool {
		a, b := subcmds[i], subcmds[j]
		if a[1] == "" && b[1] == "" {
			return a[0] < b[0]
		}
		if a[1] != "" && b[1] != "" {
			return a[0] < b[0]
		}
		if a[1] == "" {
			return true
		}
		return false
	})

	return append(result, subcmds...)
}

func (cg *cmdGroup) printHelp(ctx context.Context, w io.Writer, cmdpath []Command) error {
	cmd := cmdpath[len(cmdpath)-1]

	usage := getUsage(cmdpath)
	help := getHelpDoc(cmd)
	subcmds := getSubcommands(cmdpath)
	flags, nflags := getFlags(cmd)
	iflags, niflags := getInheritedFlags(cmdpath)

	fmt.Fprintf(w, "Usage: %s\n", usage)
	if len(help) > 0 {
		fmt.Fprintln(w)
		// TODO: Format the help into 80 columns?
		fmt.Fprintf(w, "%s\n", help)
	}
	if len(subcmds) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Subcommands:\n")
		for _, sub := range subcmds {
			if len(sub[1]) > 0 {
				fmt.Fprintf(w, "\t%-15s  %s\n", sub[0], sub[1])
			} else if len(sub[0]) > 0 {
				fmt.Fprintf(w, "\t%-15s\n", sub[0])
			} else {
				fmt.Fprintln(w)
			}
		}
	}
	if nflags > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Flags:\n")
		flags.SetOutput(w)
		flags.PrintDefaults()
	}
	if niflags > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Inherited Flags:\n")
		iflags.SetOutput(w)
		iflags.PrintDefaults()
	}
	return nil
}
