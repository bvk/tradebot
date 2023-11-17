// Copyright (c) 2023 BVK Chaitaya

package api

import (
	"fmt"

	"github.com/google/uuid"
)

const JobRenamePath = "/trader/job/rename"

type JobRenameRequest struct {
	UID     string
	OldName string

	NewName string
}

type JobRenameResponse struct {
	UID string
}

func (r *JobRenameRequest) Check() error {
	if len(r.UID) == 0 && len(r.OldName) == 0 {
		return fmt.Errorf("one of UID or OldName must be set")
	}
	if len(r.UID) != 0 && len(r.OldName) != 0 {
		return fmt.Errorf("only one of UID or OldName must be set")
	}
	if len(r.UID) != 0 {
		if _, err := uuid.Parse(r.UID); err != nil {
			return fmt.Errorf("UID value must be an uuid")
		}
	}
	if len(r.NewName) == 0 {
		return fmt.Errorf("NewName cannot be empty")
	}
	return nil
}
