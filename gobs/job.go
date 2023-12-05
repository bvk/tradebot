// Copyright (c) 2023 BVK Chaitanya

package gobs

type JobExportData struct {
	ID string

	State *ServerJobState

	KeyValues []*KeyValue
}
