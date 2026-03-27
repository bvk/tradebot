// Copyright (c) 2023 BVK Chaitanya

package pushover

import "fmt"

type Keys struct {
	ApplicationKey string `json:"application_key"`
	UserKey        string `json:"user_key"`
}

func (v *Keys) Check() error {
	if len(v.ApplicationKey) == 0 {
		return fmt.Errorf("application key cannot be empty")
	}
	if len(v.UserKey) == 0 {
		return fmt.Errorf("user key cannot be empty")
	}
	return nil
}
