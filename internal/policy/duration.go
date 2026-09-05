package policy

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// hoursPerDay is what a "d" suffix stands for. Go's own duration syntax stops at
// hours because a calendar day is not always 24 of them; a minimum age is a
// rule of thumb ("let a release settle for a week"), so the approximation is the
// right one here and spelling it as 168h is not.
const hoursPerDay = 24

// ParseDuration reads a min_age value: Go's duration syntax, plus a "d" suffix
// for whole days. "7d", "36h" and "90m" are all accepted; a bare number is not,
// because the unit it would mean is anybody's guess.
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Only a plain "<number>d" is translated. Anything else is handed to Go,
	// which rejects the mixed forms ("1d12h") rather than reading half of them.
	if rest, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a duration: use a Go duration or a number of days, e.g. 7d or 36h", s)
		}
		return time.Duration(days * hoursPerDay * float64(time.Hour)), nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration: use a Go duration or a number of days, e.g. 7d or 36h", s)
	}
	return d, nil
}
