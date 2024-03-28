// Copyright (c) 2024 BVK Chaitanya

package timerange

import (
	"math"
	"time"
)

type Range struct {
	Begin, End time.Time
}

func (r *Range) Equal(v *Range) bool {
	return r.Begin.Equal(v.Begin) && r.End.Equal(v.End)
}

func (r *Range) IsZero() bool {
	return r.Begin.IsZero() && r.End.IsZero()
}

func (r *Range) InRange(v time.Time) bool {
	if r.IsZero() {
		return true
	}
	if !r.Begin.IsZero() && v.Before(r.Begin) {
		return false
	}
	if !r.End.IsZero() && (v.Equal(r.End) || v.After(r.End)) {
		return false
	}
	return true
}

func (r *Range) Duration() time.Duration {
	if r.IsZero() {
		return math.MaxInt64
	}
	if r.End.IsZero() {
		return time.Since(r.Begin)
	}
	return r.End.Sub(r.Begin)
}

func (r *Range) clone() *Range {
	return &Range{
		Begin: r.Begin,
		End:   r.End,
	}
}

func minTime(a, b time.Time) time.Time {
	if a.IsZero() || b.IsZero() {
		return time.Time{}
	}
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.IsZero() || b.IsZero() {
		return time.Time{}
	}
	if a.After(b) {
		return a
	}
	return b
}

func Union(a, b *Range) *Range {
	if a.IsZero() {
		return b.clone()
	}
	if b.IsZero() {
		return a.clone()
	}
	return &Range{
		Begin: minTime(a.Begin, b.Begin),
		End:   maxTime(a.End, b.End),
	}
}
