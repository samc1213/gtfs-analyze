package infra

import (
	"fmt"
	"time"
)

type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// TODO: Consider caching this time.Date instance
func (date *Date) Weekday() time.Weekday {
	return time.Date(date.Year, date.Month, date.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

func (date *Date) String() string {
	return fmt.Sprint(date.Month.String(), "-", date.Day, "-", date.Year)
}
