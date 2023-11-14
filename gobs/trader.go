// Copyright (c) 2023 BVK Chaitanya

package gobs

import "github.com/bvk/tradebot/job"

type TraderJobState struct {
	State job.State

	CurrentState string

	NeedsManualResume bool
}
