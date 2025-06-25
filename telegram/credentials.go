// Copyright (c) 2025 BVK Chaitanya

package telegram

import (
	"fmt"
	"slices"
)

type Secrets struct {
	BotToken string `json:"token"`

	OwnerID string `json:"owner"`

	AdminID string `json:"admin"`

	OtherIDs []string `json:"others"`
}

func (v *Secrets) Check() error {
	if len(v.BotToken) == 0 {
		return fmt.Errorf("bot token cannot be empty")
	}
	if len(v.OwnerID) == 0 {
		return fmt.Errorf("owner id cannot be empty")
	}
	if slices.Contains(v.OtherIDs, "") {
		return fmt.Errorf("empty string in other ids is not a valid id")
	}
	if slices.Contains(v.OtherIDs, v.AdminID) {
		return fmt.Errorf("admin id should not be repeated in other ids")
	}
	if slices.Contains(v.OtherIDs, v.OwnerID) {
		return fmt.Errorf("owner id should not be repeated in other ids")
	}
	return nil
}

func (v *Secrets) Clone() *Secrets {
	return &Secrets{
		BotToken: v.BotToken,
		OwnerID:  v.OwnerID,
		AdminID:  v.AdminID,
		OtherIDs: slices.Clone(v.OtherIDs),
	}
}
