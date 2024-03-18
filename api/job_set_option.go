// Copyright (c) 2023 BVK Chaitanya

package api

import "fmt"

const JobSetOptionPath = "/trader/job/set-option"

type JobSetOptionRequest struct {
	UID string

	OptionKey   string
	OptionValue string
}

type JobSetOptionResponse struct {
}

func (req *JobSetOptionRequest) Check() error {
	if len(req.UID) == 0 {
		return fmt.Errorf("job uid cannot be empty")
	}
	if len(req.OptionKey) == 0 {
		return fmt.Errorf("option key cannot be empty")
	}
	return nil
}
