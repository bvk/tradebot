// Copyright (c) 2023 BVK Chaitanya

package api

const JobListPath = "/trader/job/list"

type JobListRequest struct {
}

type JobListResponseItem struct {
	UID   string
	Type  string
	State string
	Name  string
}

type JobListResponse struct {
	Jobs []*JobListResponseItem
}
