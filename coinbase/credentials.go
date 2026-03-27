// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "fmt"

type Credentials struct {
	KID string `json:"kid"`
	PEM string `json:"pem"`
}

func (v *Credentials) Check() error {
	if len(v.KID) == 0 {
		return fmt.Errorf("key id cannot be empty")
	}
	if len(v.PEM) == 0 {
		return fmt.Errorf("PEM cannot be empty")
	}
	return nil
}
