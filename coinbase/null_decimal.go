// Copyright (c) 2023 BVK Chaitanya

package coinbase

import "github.com/shopspring/decimal"

type NullDecimal struct {
	Decimal decimal.Decimal
}

func (v *NullDecimal) UnmarshalJSON(raw []byte) error {
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

func (v NullDecimal) MarshalJSON() ([]byte, error) {
	return v.Decimal.MarshalJSON()
}
