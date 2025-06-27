// Copyright (c) 2025 BVK Chaitanya

package timerange

import "testing"

func TestUtil(t *testing.T) {
	v := Today(nil)
	t.Logf("Today: Begin=%v End=%v", v.Begin, v.End)

	v = Yesterday(nil)
	t.Logf("Yesterday: Begin=%v End=%v", v.Begin, v.End)

	v = ThisWeek(nil)
	t.Logf("ThisWeek: Begin=%v End=%v", v.Begin, v.End)

	v = LastWeek(nil)
	t.Logf("LastWeek: Begin=%v End=%v", v.Begin, v.End)

	v = ThisMonth(nil)
	t.Logf("ThisMonth: Begin=%v End=%v", v.Begin, v.End)

	v = LastMonth(nil)
	t.Logf("LastMonth: Begin=%v End=%v", v.Begin, v.End)

	v = ThisYear(nil)
	t.Logf("ThisYear: Begin=%v End=%v", v.Begin, v.End)

	v = LastYear(nil)
	t.Logf("LastYear: Begin=%v End=%v", v.Begin, v.End)

	v = Lifetime(nil)
	t.Logf("Lifetime: Begin=%v End=%v", v.Begin, v.End)
}
