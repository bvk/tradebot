// Copyright (c) 2023 BVK Chaitanya

package gobs

type JobExportData struct {
	ID string

	Name     string
	Typename string

	State *ServerJobState

	KeyValues []*KeyValue
}
