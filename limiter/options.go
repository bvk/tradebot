// Copyright (c) 2024 BVK Chaitanya

package limiter

import "fmt"

func (v *Limiter) SetOption(opt, value string) (string, error) {
	return "", fmt.Errorf("limiter option %q is invalid", opt)
}
