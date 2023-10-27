// Copyright (c) 2023 BVK Chaitanya

package exchange

import "time"

type RemoteTime time.Time

func (v RemoteTime) MarshalBinary() ([]byte, error) {
	s := time.Time(v).Format(time.RFC3339Nano)
	return []byte(s), nil
}

func (v *RemoteTime) UnmarshalBinary(bs []byte) error {
	t, err := time.Parse(time.RFC3339Nano, string(bs))
	if err != nil {
		return err
	}
	*v = RemoteTime(t)
	return nil
}
