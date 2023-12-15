// Copyright (c) 2023 BVK Chaitanya

package gobs

type JobExportData struct {
	UID      string
	Name     string
	Typename string

	JobFlags uint64
	JobState string

	KeyValues []*KeyValue
}

type JobData struct {
	ID       string
	Typename string
	Flags    uint64

	State string
}
