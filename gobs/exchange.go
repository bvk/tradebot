// Copyright (c) 2023 BVK Chaitanya

package gobs

import (
	"time"

	"github.com/shopspring/decimal"
)

type RemoteTime struct {
	time.Time
}

type Order struct {
	ServerOrderID string
	ClientOrderID string
	CreateTime    RemoteTime

	Side   string
	Status string

	FilledFee   decimal.Decimal
	FilledSize  decimal.Decimal
	FilledPrice decimal.Decimal

	Done       bool
	DoneReason string
}
