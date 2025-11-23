// Copyright (c) 2023 BVK Chaitanya

package gobs

type State string

const (
	PAUSED    State = "PAUSED"
	RUNNING   State = "RUNNING"
	COMPLETED State = "COMPLETED"
	CANCELED  State = "CANCELED"
	FAILED    State = "FAILED"
)

// IsPaused returns true if job is not complete and can be resumed later.
func (s State) IsPaused() bool {
	return len(s) == 0 || string(s) == "PAUSED"
}

// IsRunning returns true if job is expected to be running.
func (s State) IsRunning() bool {
	return s == RUNNING
}

// IsDone returns true if job is complete and cannot be run anymore.
func (s State) IsDone() bool {
	return s == COMPLETED || s == CANCELED || s == FAILED
}

type JobExportData struct {
	UID      string
	Name     string
	Typename string

	JobFlags uint64
	JobState State

	KeyValues []*KeyValue
}

type JobData struct {
	ID       string
	Typename string
	Flags    uint64

	State State
}
