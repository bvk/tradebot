// Copyright (c) 2025 BVK Chaitanya

package envfile

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

type options struct {
	variableNamePrefix string

	searchCurrentDirectory bool

	scanParentDirectories bool

	overwriteIfExists bool
}

// UpdateEnv updates current process's environment with the values read from
// the env filename found in the user's home directory. The location of the env
// file search path and other behaviors can be changed by the input options.
//
// Please note that, NO shell escaping or expansion or ignoring # comments is
// performed on environment variable values found in the env file.
func UpdateEnv(filename string, opts ...Option) error {
	if strings.ContainsRune(filename, os.PathSeparator) {
		return fmt.Errorf("file name contains path separator: %w", os.ErrInvalid)
	}
	var fopts options
	for _, v := range opts {
		if err := v.apply(&fopts); err != nil {
			return err
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	var fpaths []string
	if fopts.searchCurrentDirectory {
		fpaths = []string{filepath.Join(cwd, filename)}
	}
	if fopts.scanParentDirectories {
		last, dir := "", filepath.Dir(cwd)
		for dir != last {
			fpaths = append(fpaths, filepath.Join(dir, filename))
			last, dir = dir, filepath.Dir(dir)
		}
	}
	if len(fpaths) == 0 {
		user, err := user.Current()
		if err != nil {
			return err
		}
		if len(user.HomeDir) == 0 {
			return fmt.Errorf("could not determine current user's home directory")
		}
		fpaths = []string{filepath.Join(user.HomeDir, filename)}
	}
	for _, fpath := range fpaths {
		fp, err := os.Open(fpath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			continue
		}
		defer fp.Close()

		scanner := bufio.NewScanner(fp)
		for i := 1; scanner.Scan(); i++ {
			line := string(bytes.TrimSpace(scanner.Bytes()))
			if len(line) == 0 {
				continue
			}
			p := strings.IndexRune(line, '=')
			if p == -1 {
				return fmt.Errorf("invalid/unrecognized variable assignment on line %d: %w", i, os.ErrInvalid)
			}
			key, value := line[:p], line[p+1:]
			if !prefixRe.MatchString(key) {
				return fmt.Errorf("invalid environment variable name %q on line %d: %w", key, i, os.ErrInvalid)
			}
			key = fopts.variableNamePrefix + key
			if len(os.Getenv(key)) != 0 {
				if !fopts.overwriteIfExists {
					continue
				}
			}
			os.Setenv(key, value)
		}
		break
	}
	return nil
}
