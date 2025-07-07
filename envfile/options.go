// Copyright (c) 2025 BVK Chaitanya

package envfile

import (
	"fmt"
	"os"
	"regexp"
)

type Option interface {
	apply(*options) error
}

type optionFunc func(*options) error

func (v optionFunc) apply(opts *options) error {
	return v(opts)
}

// SearchCurrentDir option if present will search for the environment file from
// current directory. If the input parameter is true, envfile search will
// include the ancestor directories up to the root directory.
func SearchCurrentDir(searchParentDirs bool) Option {
	return optionFunc(func(opts *options) error {
		opts.searchCurrentDirectory = true
		opts.scanParentDirectories = searchParentDirs
		return nil
	})
}

var prefixRe = regexp.MustCompile("^[a-zA-Z][0-9a-zA-Z_]*$")

// VariableNamePrefix option adds input prefix to all variable names defined in
// the envfile.
func VariableNamePrefix(prefix string) Option {
	return optionFunc(func(opts *options) error {
		if !prefixRe.MatchString(prefix) {
			return fmt.Errorf("variable name prefix has invalid characters: %w", os.ErrInvalid)
		}
		opts.variableNamePrefix = prefix
		return nil
	})
}

// OverwriteIfExists options allows to overwrite or not-overwrite the current
// value for an environment variable that already has a non-empty value.
func OverwriteIfExists(overwrite bool) Option {
	return optionFunc(func(opts *options) error {
		opts.overwriteIfExists = overwrite
		return nil
	})
}
