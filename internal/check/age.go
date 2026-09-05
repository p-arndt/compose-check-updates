package check

import (
	"fmt"
	"time"
)

// The thresholds a relative age is rounded at. Months and years are the
// approximate ones every "x ago" is: nobody reading "2mo ago" beside a tag is
// counting the days, and the exact date is in the JSON output for whoever is.
const (
	day   = 24 * time.Hour
	month = 30 * day
	year  = 365 * day
)

// Age renders how long before now the tag was published, short enough to sit
// beside a tag in an aligned report. A zero or future time renders as "",
// because an age ccu does not know must not read as one it does.
func Age(published, now time.Time) string {
	if published.IsZero() {
		return ""
	}

	d := now.Sub(published)
	switch {
	case d < 0:
		// A clock skewed the wrong way, or a tag stamped with a build date in the
		// future. Reporting "-3d ago" would only look broken.
		return ""
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < day:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < month:
		return fmt.Sprintf("%dd ago", int(d/day))
	case d < year:
		return fmt.Sprintf("%dmo ago", int(d/month))
	default:
		return fmt.Sprintf("%dy ago", int(d/year))
	}
}
