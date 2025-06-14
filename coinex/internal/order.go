// Copyright (c) 2025 BVK Chaitanya

package internal

import (
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bvk/tradebot/exchange"
	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Order struct {
	OrderID          int64           `json:"order_id"`
	ClientOrderID    string          `json:"client_id"`
	Market           string          `json:"market"`
	MarketType       string          `json:"market_type"`
	Side             string          `json:"side"`
	OrderType        string          `json:"type"`
	Currency         string          `json:"ccy"`
	OrderAmount      decimal.Decimal `json:"amount"`
	OrderPrice       decimal.Decimal `json:"price"`
	UnfilledAmount   decimal.Decimal `json:"unfilled_amount"`
	FilledAmount     decimal.Decimal `json:"filled_amount"`
	FilledValue      decimal.Decimal `json:"filled_value"`
	LastFilledAmount decimal.Decimal `json:"last_filled_amount"`
	LastFilledPrice  decimal.Decimal `json:"last_filled_price"`
	BaseFee          decimal.Decimal `json:"base_fee"`
	QuoteFee         decimal.Decimal `json:"quote_fee"`
	DiscountFee      decimal.Decimal `json:"discount_fee"`
	MakerFeeRate     decimal.Decimal `json:"maker_fee_rate"`
	TakerFeeRate     decimal.Decimal `json:"taker_fee_rate"`
	CreatedAtMilli   int64           `json:"created_at"`
	UpdatedAtMilli   int64           `json:"updated_at"`
	Status           string          `json:"status"`

	HasFinishEvent bool `json:"-"`
}

var _ exchange.OrderUpdate = &Order{}
var _ exchange.Order = &Order{}
var _ exchange.OrderDetail = &Order{}

func (v *Order) ServerID() string {
	return strconv.FormatInt(v.OrderID, 10)
}

func (v *Order) ClientID() uuid.UUID {
	var cuuid uuid.UUID
	bs, err := hex.DecodeString(v.ClientOrderID)
	if err != nil {
		panic(err)
	}
	copy(cuuid[:], bs)
	return cuuid
}

func (v *Order) OrderSide() string {
	return strings.ToUpper(v.Side)
}

func (v *Order) CreatedAt() gobs.RemoteTime {
	return gobs.RemoteTime{Time: time.UnixMilli(v.CreatedAtMilli)}
}

func (v *Order) UpdatedAt() gobs.RemoteTime {
	return gobs.RemoteTime{Time: time.UnixMilli(v.UpdatedAtMilli)}
}

func (v *Order) ExecutedSize() decimal.Decimal {
	return v.FilledAmount
}

func (v *Order) ExecutedValue() decimal.Decimal {
	return v.FilledValue
}

func (v *Order) ExecutedFee() decimal.Decimal {
	return v.QuoteFee // FIXME: Is this in-sync with BaseFee?
}

func (v *Order) Size() decimal.Decimal {
	return v.OrderAmount
}

func (v *Order) Price() decimal.Decimal {
	return v.OrderPrice
}

func (v *Order) OrderStatus() string {
	return v.Status
}

func (v *Order) AddUpdate(other *Order) error {
	if v.OrderID != other.OrderID {
		return os.ErrInvalid
	}
	if v.ClientOrderID != other.ClientOrderID {
		return os.ErrInvalid
	}
	if v.Market != other.Market {
		return fmt.Errorf("market ids do not match")
	}
	if v.MarketType != other.MarketType {
		return fmt.Errorf("market type ids do not match")
	}
	if v.Side != other.Side {
		return fmt.Errorf("order sides do not match")
	}
	if v.OrderType != other.OrderType {
		return fmt.Errorf("order types do not match")
	}
	if v.Currency != other.Currency {
		return fmt.Errorf("order currencies do not match")
	}
	if v.OrderAmount != other.OrderAmount {
		return fmt.Errorf("order amounts do not match")
	}
	if v.OrderPrice != other.OrderPrice {
		return fmt.Errorf("order prices do not match")
	}
	if v.CreatedAtMilli == 0 && other.CreatedAtMilli != 0 {
		v.CreatedAtMilli = other.CreatedAtMilli
	}
	if v.CreatedAtMilli != 0 && other.CreatedAtMilli != 0 {
		if v.CreatedAtMilli != other.CreatedAtMilli {
			slog.Warn("order create times do not match", "known", v.CreatedAtMilli, "update", other.CreatedAtMilli)
			return fmt.Errorf("order create times do not match")
		}
	}
	if v.UnfilledAmount.GreaterThan(other.UnfilledAmount) {
		v.UnfilledAmount = other.UnfilledAmount
	}
	if v.FilledAmount.LessThan(other.FilledAmount) {
		v.FilledAmount = other.FilledAmount
	}
	if v.FilledValue.LessThan(other.FilledValue) {
		v.FilledValue = other.FilledValue
	}
	if v.BaseFee.LessThan(other.BaseFee) {
		v.BaseFee = other.BaseFee
	}
	if v.QuoteFee.LessThan(other.QuoteFee) {
		v.QuoteFee = other.QuoteFee
	}
	if v.DiscountFee.LessThan(other.DiscountFee) {
		v.DiscountFee = other.DiscountFee
	}
	if v.UpdatedAtMilli < other.UpdatedAtMilli {
		v.UpdatedAtMilli = other.UpdatedAtMilli
		v.LastFilledAmount = other.LastFilledAmount
		v.LastFilledPrice = other.LastFilledPrice
	}
	if !v.IsDone() && other.IsDone() {
		v.Status = other.Status
		v.HasFinishEvent = other.HasFinishEvent
	}
	if v.IsDone() && other.IsDone() {
		if !strings.EqualFold(v.Status, "filled") && !strings.EqualFold(v.Status, "canceled") {
			if strings.EqualFold(other.Status, "filled") || strings.EqualFold(other.Status, "canceled") {
				v.Status = other.Status
			}
		}
	}
	return nil
}

func (v *Order) IsDone() bool {
	if strings.EqualFold(v.Status, "filled") || strings.EqualFold(v.Status, "canceled") {
		return true
	}
	if v.HasFinishEvent {
		return true
	}
	return false
}

func (v *Order) FinishedAt() gobs.RemoteTime {
	if v.IsDone() {
		return v.UpdatedAt()
	}
	return gobs.RemoteTime{}
}
