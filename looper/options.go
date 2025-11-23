// Copyright (c) 2024 BVK Chaitanya

package looper

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

var trues = []string{"true", "yes", "1"}
var falses = []string{"false", "no", "0"}

func (v *Looper) SetOption(opt, val string) (string, error) {
	switch key := strings.ToLower(opt); key {
	case "retire":
		return v.setRetireOption(key, val)
	default:
		return "", fmt.Errorf("invalid/unsupported looper option %q", key)
	}
}

func (v *Looper) setRetireOption(opt, val string) (string, error) {
	// This retire option can be set to "true", but cannot be unset after the job
	// has started already. So, we return "undo" to indicate the undo action
	// separate from a "false" value.

	value := strings.ToLower(val)
	if slices.Contains(trues, value) {
		if !v.retireOpt {
			v.retireOpt = true
			return "undo", nil
		}
		return "true", nil
	}

	if slices.Contains(falses, value) {
		if v.retireOpt {
			return "", fmt.Errorf("retire option cannot be undone")
		}
		return "false", nil
	}

	if value == "undo" {
		if v.retireOpt {
			v.retireOpt = false
		} else {
			slog.Error("attempts to undo a retire operation when it is already false are unexpected (ignored)")
		}
		return "undo", nil
	}
	return "", fmt.Errorf("invalid value %q for the retire-option", value)
}
