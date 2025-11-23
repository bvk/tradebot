// Copyright (c) 2024 BVK Chaitanya

package waller

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

func (w *Waller) SetOption(opt, val string) (_ string, status error) {
	switch key := strings.ToLower(opt); key {
	case "retire":
		return w.setRetireOption(key, val)
	default:
		return "", fmt.Errorf("waller option %q is invalid", key)
	}
}

var trues = []string{"true", "yes", "1"}
var falses = []string{"false", "no", "0"}

func (w *Waller) setRetireOption(opt, val string) (_ string, status error) {
	undo := ""
	value := strings.ToLower(val)
	switch {
	case slices.Contains(trues, value):
		undo = "false"
	case slices.Contains(falses, value):
		undo = "true"
	default:
		return "", fmt.Errorf("retire value %q is invalid", value)
	}

	for _, loop := range w.loopers {
		loop := loop
		undoValue, err := loop.SetOption(opt, val)
		if err != nil {
			return "", err
		}
		defer func() {
			if status != nil {
				if _, err := loop.SetOption(opt, undoValue); err != nil {
					slog.Error("could not undo set-option on looper (needs manual fix)", "looper", loop, "opt", opt, "val", val, "err", err)
				}
			}
		}()
	}

	return undo, nil
}
