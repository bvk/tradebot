// Copyright (c) 2025 BVK Chaitanya

package timerange

import (
	"time"
)

func Lifetime(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	return &Range{
		Begin: time.Date(2000, 9, 24, 0, 0, 0, 0, zone),
		End:   time.Date(2100, 9, 24, 0, 0, 0, 0, zone),
	}
}

func Today(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	beg := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	return &Range{
		Begin: beg,
		End:   beg.Add(24 * time.Hour),
	}
}

func Yesterday(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	return &Range{
		Begin: today.Add(-24 * time.Hour),
		End:   today,
	}
}

func ThisWeek(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone)
	begin := today.AddDate(0, 0, -int(now.Weekday()))
	end := begin.AddDate(0, 0, 7)
	return &Range{Begin: begin, End: end}
}

func LastWeek(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone).AddDate(0, 0, -int(now.Weekday()))
	begin := end.AddDate(0, 0, -7)
	return &Range{Begin: begin, End: end}
}

func ThisMonth(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	year, month := now.Year(), now.Month()
	begin := time.Date(year, month, 1, 0, 0, 0, 0, zone)
	end := begin.AddDate(0, 1, 0)
	return &Range{Begin: begin, End: end}
}

func LastMonth(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	year, month := now.Year(), now.Month()
	end := time.Date(year, month, 1, 0, 0, 0, 0, zone)
	begin := end.AddDate(0, -1, 0)
	return &Range{Begin: begin, End: end}
}

func ThisYear(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	begin := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, zone)
	return &Range{Begin: begin, End: begin.AddDate(1, 0, 0)}
}

func LastYear(zone *time.Location) *Range {
	if zone == nil {
		zone = time.Local
	}
	now := time.Now().In(zone)
	end := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, zone)
	begin := end.AddDate(-1, 0, 0)
	return &Range{Begin: begin, End: end}
}
