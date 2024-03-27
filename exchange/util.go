// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"fmt"
	"time"

	"github.com/bvk/tradebot/gobs"
	"github.com/shopspring/decimal"
)

func Equal(a, b *Order) bool {
	return a.OrderID == b.OrderID &&
		a.ClientOrderID == b.ClientOrderID &&
		a.Side == b.Side &&
		a.CreateTime.Time.Equal(b.CreateTime.Time) &&
		a.Fee.Equal(b.Fee) &&
		a.FilledSize.Equal(b.FilledSize) &&
		a.FilledPrice.Equal(b.FilledPrice) &&
		a.Status == b.Status &&
		a.Done == b.Done &&
		a.DoneReason == b.DoneReason
}

func Merge(known, update *Order) *Order {
	if known.OrderID != update.OrderID {
		return known
	}

	tmp := new(Order)
	*tmp = *known

	if known.ClientOrderID == "" && update.ClientOrderID != "" {
		tmp.ClientOrderID = update.ClientOrderID
	}
	if known.Side == "" && update.Side != "" {
		tmp.Side = update.Side
	}
	if known.CreateTime.IsZero() && !update.CreateTime.IsZero() {
		tmp.CreateTime = update.CreateTime
	}
	if known.Fee.IsZero() && !update.Fee.IsZero() {
		tmp.Fee = update.Fee
	}
	if known.Fee.LessThan(update.Fee) {
		tmp.Fee = update.Fee
	}
	if known.FilledSize.LessThan(update.FilledSize) {
		tmp.FilledSize = update.FilledSize
		tmp.FilledPrice = update.FilledPrice
	}
	if known.FilledPrice.IsZero() && !update.FilledPrice.IsZero() {
		tmp.FilledPrice = update.FilledPrice
	}
	if known.Status == "" && update.Status != "" {
		tmp.Status = update.Status
	}
	if known.Status == "OPEN" && update.Status == "CANCELLED" {
		tmp.Status = update.Status
	}
	if known.Status != "FILLED" && update.Status == "FILLED" {
		tmp.Status = update.Status
	}
	if !known.Done && update.Done {
		tmp.Done = update.Done
	}
	if known.DoneReason == "" && update.DoneReason != "" {
		tmp.DoneReason = update.DoneReason
	}
	return tmp
}

func (v *Order) String() string {
	return fmt.Sprintf("{ID: %s ClientID %s Side %s Price %s Size %s Fee %s Status %s CreatedAt %s}",
		v.OrderID, v.ClientOrderID, v.Side, v.FilledPrice.StringFixed(3), v.FilledSize.StringFixed(3), v.Fee.StringFixed(3), v.Status, v.CreateTime.Time.Format(time.DateTime))
}

func FilledSize(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledSize)
	}
	return sum
}

func FilledValue(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledSize.Mul(v.FilledPrice))
	}
	return sum
}

func FilledFee(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledFee)
	}
	return sum
}

func AvgPrice(vs []*gobs.Order) decimal.Decimal {
	var sum decimal.Decimal
	for _, v := range vs {
		sum = sum.Add(v.FilledPrice)
	}
	return sum.Div(decimal.NewFromInt(int64(len(vs))))
}

func MaxPrice(vs []*gobs.Order) decimal.Decimal {
	var max decimal.Decimal
	for i, v := range vs {
		if i == 0 {
			max = v.FilledPrice
			continue
		}
		if v.FilledPrice.GreaterThan(max) {
			max = v.FilledPrice
		}
	}
	return max
}

func MinPrice(vs []*gobs.Order) decimal.Decimal {
	var min decimal.Decimal
	for i, v := range vs {
		if i == 0 {
			min = v.FilledPrice
			continue
		}
		if v.FilledPrice.LessThan(min) {
			min = v.FilledPrice
		}
	}
	return min
}
