package page

import "time"

// When is a time as a page shows it: the time of day when it is today, and
// the date before it on any other day.
func When(t, now time.Time) string {
	t = t.Local()
	if y, m, d := now.Local().Date(); t.Year() == y && t.Month() == m && t.Day() == d {
		return t.Format("15:04")
	}
	return t.Format("2 Jan 15:04")
}
