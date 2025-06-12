// Copyright (c) 2025 BVK Chaitanya

package exchange

import (
	"fmt"
	"os"
	"strings"

	"github.com/bvk/tradebot/gobs"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var lastUUID = uuid.Must(uuid.Parse("ffffffff-ffff-ffff-ffff-ffffffffffff"))

type SimpleOrder struct {
	ServerOrderID OrderID

	ClientUUID uuid.UUID

	Side string

	CreateTime gobs.RemoteTime
	FinishTime gobs.RemoteTime

	Fee         decimal.Decimal
	FilledSize  decimal.Decimal
	FilledPrice decimal.Decimal

	Status string

	// Done is true if order is complete. DoneReason below indicates if order has
	// failed or succeeded.
	Done bool

	// When Done is true, an empty DoneReason value indicates a successfull
	// execution of the order and a non-empty DoneReason indicates a failure with
	// the reason for the failure.
	DoneReason string
}

var _ Order = &SimpleOrder{}
var _ OrderUpdate = &SimpleOrder{}
var _ OrderDetail = &SimpleOrder{}

func NewSimpleOrder(serverID string, clientID uuid.UUID, side string) (*SimpleOrder, error) {
	if len(serverID) == 0 {
		return nil, fmt.Errorf("NewSimpleOrder: server id cannot be empty")
	}
	if !strings.EqualFold(side, "buy") && !strings.EqualFold(side, "sell") {
		return nil, fmt.Errorf("NewSimpleOrder: side %q is invalid/unsupported", side)
	}
	if clientID == uuid.Nil {
		return nil, fmt.Errorf("NewSimpleOrder: zero uuid is not a valid client id")
	}
	if clientID == lastUUID {
		return nil, fmt.Errorf("NewSimpleOrder: all ones uuid is not a valid client id")
	}
	return &SimpleOrder{
		ServerOrderID: OrderID(serverID),
		ClientUUID:    clientID,
		Side:          strings.ToUpper(side),
	}, nil
}

func SimpleOrderFromOrderDetail(detail OrderDetail) (*SimpleOrder, error) {
	s, err := NewSimpleOrder(detail.ServerID(), detail.ClientID(), detail.OrderSide())
	if err != nil {
		return nil, fmt.Errorf("SimpleOrderFromOrderDetail: %w", err)
	}
	s.CreateTime = detail.CreatedAt()
	s.FinishTime = detail.FinishedAt()
	s.Fee = detail.ExecutedFee()
	s.FilledSize = detail.ExecutedSize()
	if !s.FilledSize.IsZero() {
		s.FilledPrice = detail.ExecutedValue().Div(detail.ExecutedSize())
	}
	s.Status = detail.OrderStatus()
	s.Done = detail.IsDone()
	if s.Done {
		s.DoneReason = detail.OrderStatus()
	}
	return s, nil
}

func (v *SimpleOrder) ServerID() string {
	return string(v.ServerOrderID)
}

func (v *SimpleOrder) ClientID() uuid.UUID {
	return v.ClientUUID
}

func (v *SimpleOrder) OrderSide() string {
	return strings.ToUpper(v.Side)
}

func (v *SimpleOrder) OrderStatus() string {
	return v.Status
}

func (v *SimpleOrder) CreatedAt() gobs.RemoteTime {
	return gobs.RemoteTime{Time: v.CreateTime.Time}
}

func (v *SimpleOrder) UpdatedAt() gobs.RemoteTime {
	if v.FinishTime.Time.IsZero() {
		return v.CreatedAt()
	}
	return gobs.RemoteTime{Time: v.FinishTime.Time}
}

func (v *SimpleOrder) FinishedAt() gobs.RemoteTime {
	if v.IsDone() {
		return v.UpdatedAt()
	}
	return gobs.RemoteTime{}
}

func (v *SimpleOrder) ExecutedFee() decimal.Decimal {
	return v.Fee
}

func (v *SimpleOrder) ExecutedSize() decimal.Decimal {
	return v.FilledSize
}

func (v *SimpleOrder) ExecutedValue() decimal.Decimal {
	return v.FilledSize.Mul(v.FilledPrice)
}

func (v *SimpleOrder) IsDone() bool {
	return v.Done
}

func (v *SimpleOrder) AddUpdate(update OrderUpdate) error {
	if v.ServerID() != update.ServerID() {
		return os.ErrInvalid
	}
	if v.ClientID() != update.ClientID() {
		return os.ErrInvalid
	}

	ctime := update.CreatedAt()
	if !v.CreateTime.Time.IsZero() && !ctime.Time.IsZero() {
		if !v.CreateTime.Time.Equal(ctime.Time) {
			return fmt.Errorf("create times do not match")
		}
	}
	if v.CreateTime.Time.IsZero() && !ctime.Time.IsZero() {
		v.CreateTime.Time = ctime.Time
	}

	if v.Fee.LessThan(update.ExecutedFee()) {
		v.Fee = update.ExecutedFee()
	}
	if !update.ExecutedSize().IsZero() {
		if v.FilledSize.LessThan(update.ExecutedSize()) {
			v.FilledSize = update.ExecutedSize()
			v.FilledPrice = update.ExecutedValue().Div(update.ExecutedSize())
		}
	}
	if !v.Done && update.IsDone() {
		v.Done = true
		v.Status = update.OrderStatus()
		if x, ok := update.(*SimpleOrder); ok {
			v.DoneReason = x.DoneReason
		} else {
			v.DoneReason = update.OrderStatus()
		}
	}
	return nil
}
