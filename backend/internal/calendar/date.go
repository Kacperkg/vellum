// Package calendar handles financial calendar days independently of instants.
package calendar

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Day keeps the written calendar day of an RFC3339 value. It does not convert
// that value to UTC first. New financial records store this day at UTC midnight.
func Day(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

var format = regexp.MustCompile(`^([0-9]{4}-[0-9]{2}-[0-9]{2})(T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9]))?$`)

func Valid(value time.Time) bool { return !value.IsZero() && value.Year() >= 1 && value.Year() <= 9999 }

func Parse(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, value)
	}
	if err != nil || !Valid(Day(parsed)) || !format.MatchString(value) {
		return time.Time{}, fmt.Errorf("date must be YYYY-MM-DD or RFC3339 in years 0001 through 9999")
	}
	return Day(parsed), nil
}

// Date is only an HTTP input type; model timestamps and auth expiry stay time.Time.
type Date struct{ time.Time }

func (d *Date) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("date must be a string")
	}
	parsed, err := Parse(value)
	if err != nil {
		return err
	}
	d.Time = parsed
	return nil
}
