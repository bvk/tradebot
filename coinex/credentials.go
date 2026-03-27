// Copyright (c) 2025 BVK Chaitanya

package coinex

import "fmt"

type Credentials struct {
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

func (v *Credentials) Check() error {
	if len(v.Key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}
	if len(v.Secret) == 0 {
		return fmt.Errorf("secret cannot be empty")
	}
	return nil
}
