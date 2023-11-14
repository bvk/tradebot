// Copyright (c) 2023 BVK Chaitanya

package exchange

import (
	"fmt"
	"strings"
	"time"
)

type RemoteTime struct {
	time.Time

	Format string
}

func (v RemoteTime) Unix() int64 {
	return v.Time.Unix()
}

func (v *RemoteTime) UnmarshalJSON(raw []byte) error {
	s := strings.Trim(string(raw), `"`)
	if s == "null" || s == "" {
		v.Time = time.Time{}
		return nil
	}
	format := time.RFC3339Nano
	if v.Format != "" {
		format = v.Format
	}
	t, err := time.Parse(format, maybeUnquote(s))
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
	format := time.RFC3339Nano
	if v.Format != "" {
		format = v.Format
	}
	return []byte(fmt.Sprintf(`"%s"`, v.Time.Format(format))), nil
}

func maybeUnquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
