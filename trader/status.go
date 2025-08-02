// Copyright (c) 2023 BVK Chaitanya

package trader

import (
	"fmt"

	"github.com/bvk/tradebot/timerange"
)

type Status struct {
	*Summary

	UID          string
	ProductID    string
	ExchangeName string
}

func (s *Status) String() string {
	return fmt.Sprintf("uid %s product %s bvalue %s s %s usize %s", s.UID, s.ProductID, s.BoughtSize, s.SoldSize, s.UnsoldSize)
}

type Statuser interface {
	Status(*timerange.Range) *Status
}
