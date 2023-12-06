// Copyright (c) 2023 BVK Chaitaya

package api

import (
	"fmt"
)

const SetJobNamePath = "/trader/set-job-name"

type SetJobNameRequest struct {
	UID string

	JobName string
}

type SetJobNameResponse struct {
}

func (r *SetJobNameRequest) Check() error {
	if len(r.UID) == 0 {
		return fmt.Errorf("job uid cannot be empty")
	}
	if len(r.JobName) == 0 {
		return fmt.Errorf("job name cannot be empty")
	}
	return nil
}
