// Copyright (c) 2023 BVK Chaitanya

package api

const JobResumePath = "/trader/job/resume"

type JobResumeRequest struct {
	UID string
}
type JobResumeResponse struct {
	FinalState string
}
