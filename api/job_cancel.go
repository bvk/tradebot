// Copyright (c) 2023 BVK Chaitanya

package api

const JobCancelPath = "/trader/job/cancel"

type JobCancelRequest struct {
	UID string
}
type JobCancelResponse struct {
	FinalState string
}
