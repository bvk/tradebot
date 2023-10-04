// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"fmt"
	"strings"
	"time"
)

type RemoteTime struct {
	Time time.Time
}

func (v *RemoteTime) UnmarshalJSON(raw []byte) error {
	s := strings.Trim(string(raw), `"`)
	if s == "null" || s == "" {
		v.Time = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return err
	}
	v.Time = t
	return nil
}

func (v *RemoteTime) MarshalJSON() ([]byte, error) {
	if v.Time.IsZero() {
		return []byte("null"), nil
	}
	return []byte(fmt.Sprintf(`"%s"`, v.Time.Format(time.RFC3339Nano))), nil
}
