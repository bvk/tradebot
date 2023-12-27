// Copyright (c) 2023 BVK Chaitanya

package gobs

type Action struct {
	UID string

	PairingKey string

	Point Point

	Orders []*Order
}
