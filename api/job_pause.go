// Copyright (c) 2023 BVK Chaitanya

package api

const JobPausePath = "/trader/job/pause"

type JobPauseRequest struct {
	UID string
}
type JobPauseResponse struct {
	FinalState string
}
