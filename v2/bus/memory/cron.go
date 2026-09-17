package memory

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// A small standard 5-field cron parser: minute hour day-of-month month
// day-of-week. It supports "*", "*/n", "a-b", "a-b/n", plain numbers and comma
// lists of any of those. Standard library only — a scheduler is not worth a
// dependency.
//
// Day-of-month and day-of-week follow the usual cron rule: when BOTH are
// restricted the match is their union, not their intersection.
type cronExpr struct {
	minute [60]bool
	hour   [24]bool
	dom    [32]bool
	month  [13]bool
	dow    [7]bool

	domRestricted bool
	dowRestricted bool
}

func parseCron(expr string) (*cronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, errors.New("cron expression " + strconv.Quote(expr) + " must have 5 fields: minute hour day-of-month month day-of-week")
	}
	c := &cronExpr{}
	if err := fillField(c.minute[:], fields[0], 0, 59, nil); err != nil {
		return nil, err
	}
	if err := fillField(c.hour[:], fields[1], 0, 23, nil); err != nil {
		return nil, err
	}
	if err := fillField(c.dom[:], fields[2], 1, 31, nil); err != nil {
		return nil, err
	}
	if err := fillField(c.month[:], fields[3], 1, 12, nil); err != nil {
		return nil, err
	}
	// 7 is Sunday in the common extension; fold it onto 0.
	if err := fillField(c.dow[:], fields[4], 0, 6, func(n int) int {
		if n == 7 {
			return 0
		}
		return n
	}); err != nil {
		return nil, err
	}
	c.domRestricted = fields[2] != "*"
	c.dowRestricted = fields[4] != "*"
	return c, nil
}

func fillField(set []bool, field string, min, max int, fold func(int) int) error {
	bad := func() error {
		return errors.New("cron field " + strconv.Quote(field) + " is not valid for range " +
			strconv.Itoa(min) + "-" + strconv.Itoa(max))
	}
	mark := func(n int) error {
		if fold != nil {
			n = fold(n)
		}
		if n < min || n > max || n >= len(set) {
			return bad()
		}
		set[n] = true
		return nil
	}
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return bad()
		}
		step := 1
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 {
				return bad()
			}
			step = n
			part = base
		}
		lo, hi := min, max
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			x, err1 := strconv.Atoi(a)
			y, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return bad()
			}
			if fold != nil {
				x, y = fold(x), fold(y)
			}
			if x > y {
				return bad()
			}
			lo, hi = x, y
		default:
			n, err := strconv.Atoi(part)
			if err != nil {
				return bad()
			}
			if step != 1 {
				// "5/2" is not standard cron; refuse rather than guess.
				return bad()
			}
			if err := mark(n); err != nil {
				return err
			}
			continue
		}
		if lo < min || hi > max {
			return bad()
		}
		for n := lo; n <= hi; n += step {
			if err := mark(n); err != nil {
				return err
			}
		}
	}
	for _, v := range set {
		if v {
			return nil
		}
	}
	return bad()
}

// next returns the first fire time strictly after from, searching five years
// ahead. A 29-February-only expression that cannot fire returns false rather
// than looping.
func (c *cronExpr) next(from time.Time) (time.Time, bool) {
	t := from.Truncate(time.Minute).Add(time.Minute)
	limit := from.AddDate(5, 0, 0)
	for t.Before(limit) {
		if !c.month[int(t.Month())] {
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !c.matchesDay(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if !c.hour[t.Hour()] {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location()).Add(time.Hour)
			continue
		}
		if !c.minute[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

func (c *cronExpr) matchesDay(t time.Time) bool {
	dom := c.dom[t.Day()]
	dow := c.dow[int(t.Weekday())]
	switch {
	case c.domRestricted && c.dowRestricted:
		return dom || dow
	case c.domRestricted:
		return dom
	case c.dowRestricted:
		return dow
	}
	return true
}
