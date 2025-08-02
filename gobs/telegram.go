// Copyright (c) 2025 BVK Chaitanya

package gobs

type TelegramState struct {
	LastUpdateID int

	UserChatIDMap map[string]int64
}
