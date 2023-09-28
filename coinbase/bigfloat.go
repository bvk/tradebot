// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "github.com/shopspring/decimal"

type BigFloat struct {
	decimal.Decimal
}

func (v *BigFloat) UnmarshalJSON(raw []byte) error {
	if s := string(raw); s == "" || s == `""` {
		v.Decimal = decimal.Zero
		return nil
	}
	var d decimal.Decimal
	if err := d.UnmarshalJSON(raw); err != nil {
		return err
	}
	v.Decimal = d
	return nil
}

func (v *BigFloat) MarshalJSON() ([]byte, error) {
	return v.Decimal.MarshalJSON()
}
