package alfred

import "time"

// SetUsageClock replaces the clock Usage records and ranks with.
func SetUsageClock(u *Usage, now func() time.Time) { u.now = now }
