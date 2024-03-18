// Copyright (c) 2024 BVK Chaitanya

package limiter

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

func (v *Limiter) SetOption(key, value string) error {
	optMap := map[string]func(string) error{
		"hold":                 v.setHoldOption,
		"size-limit":           v.setSizeLimitOption,
		"wait-for-ticker-side": v.setWaitForTickerSideOption,
	}
	handler, ok := optMap[key]
	if !ok {
		return fmt.Errorf("invalid option key %q", key)
	}

	if err := handler(value); err != nil {
		return err
	}
	v.optionMap[key] = value
	return nil
}

func (v *Limiter) setHoldOption(arg string) error {
	arg = strings.ToLower(arg)
	if arg == "true" {
		v.holdOpt.Store(true)
		return nil
	}
	if arg == "false" {
		v.holdOpt.Store(false)
		return nil
	}
	return fmt.Errorf(`%v: hold option only takes a "true" or "false" value`, v.uid)
}

func (v *Limiter) sizeLimit() decimal.Decimal {
	if p := v.sizeLimitOpt.Load(); p != nil {
		return p.Copy()
	}
	return v.point.Size
}

func (v *Limiter) setSizeLimitOption(value string) error {
	size, err := decimal.NewFromString(value)
	if err != nil {
		return err
	}
	if size.IsNegative() {
		return fmt.Errorf("size limit value cannot be -ve")
	}
	if size.GreaterThan(v.point.Size) {
		return fmt.Errorf("size limit value cannot be more than total size")
	}
	v.sizeLimitOpt.Store(&size)
	return nil
}

func (v *Limiter) setWaitForTickerSideOption(value string) error {
	arg := strings.ToLower(value)
	if arg == "true" {
		v.waitForTickerSideOpt.Store(true)
		return nil
	}
	if arg == "false" {
		v.waitForTickerSideOpt.Store(false)
		return nil
	}
	return fmt.Errorf(`%v: wait-for-ticker-side option only takes a "true" or "false" value`, v.uid)
}

// isTickerSideReady returns true if the input price unblocks the wait-for-ticker
// side option.
func (v *Limiter) isTickerSideReady(price decimal.Decimal) bool {
	if wait := v.waitForTickerSideOpt.Load(); !wait {
		return true
	}

	if v.point.Side() == "BUY" {
		if price.GreaterThan(v.point.Price) {
			v.waitForTickerSideOpt.Store(false)
			return true
		}
		return false
	}

	if price.LessThan(v.point.Price) {
		v.waitForTickerSideOpt.Store(false)
		return true
	}
	return false
}
