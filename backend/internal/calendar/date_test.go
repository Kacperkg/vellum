package calendar_test

import (
	"testing"
	"time"

	"github.com/kacperkg/vellum/internal/calendar"
)

func TestCalendarDatesPreserveWrittenDay(t *testing.T) {
	originalLocal := time.Local
	defer func() { time.Local = originalLocal }()
	cases := map[string]string{
		"2026-11-01T00:30:00+14:00":           "2026-11-01",
		"2026-10-31T23:30:00-12:00":           "2026-10-31",
		"2026-03-29T00:30:00Z":                "2026-03-29",
		"2026-03-29T02:30:00+01:00":           "2026-03-29",
		"2026-10-25T01:30:00+01:00":           "2026-10-25",
		"2026-10-25T01:30:00Z":                "2026-10-25",
		"2026-12-31T23:59:59.123456789-12:00": "2026-12-31",
		"2027-01-01T00:00:00+14:00":           "2027-01-01",
		"2024-02-29":                          "2024-02-29",
		"2026-04-30":                          "2026-04-30",
	}
	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			for _, zone := range []string{"Etc/GMT+12", "Pacific/Kiritimati", "Europe/London"} {
				loc, err := time.LoadLocation(zone)
				if err != nil {
					t.Fatal(err)
				}
				// Parsing does not consult the process timezone, including DST locations.
				time.Local = loc
				got, err := calendar.Parse(input)
				if err != nil || got.Format("2006-01-02") != want || got.Location() != time.UTC || got.Hour() != 0 {
					t.Fatalf("%s: %s %v", zone, got, err)
				}
			}
		})
	}
	for _, input := range []string{"2026-02-29", "2026-04-31", "2026-1-01", "2026-10-30T12:00:00", "2026-10-30T12:00:00+24:00", "2026-10-30T12:00:00+00:60", "2026-10-30T12:00:00,1Z", "0000-01-01", "0001-01-01", "0001-01-01T12:00:00Z", ""} {
		if _, err := calendar.Parse(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}
