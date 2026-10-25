package config

import (
	"reflect"
	"testing"
	"time"
)

func TestCORSOrigins(t *testing.T) {
	for _, test := range []struct {
		name, value string
		want        []string
	}{
		{"default", "", []string{"http://localhost:5173"}},
		{"blank", " , ", []string{"http://localhost:5173"}},
		{"configured", "http://localhost:15173, https://example.test,", []string{"http://localhost:15173", "https://example.test"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := corsOrigins(test.value); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("origins=%v want=%v", got, test.want)
			}
		})
	}
}

func TestRateLimitDefaultsAndOverrides(t *testing.T) {
	t.Setenv("RATE_LIMIT_LOGIN_REQUESTS", "")
	t.Setenv("RATE_LIMIT_LOGIN_WINDOW", "")
	if got := rateLimit("LOGIN", 10, time.Minute); got != (RateLimit{Requests: 10, Window: time.Minute}) {
		t.Fatalf("default rate limit = %+v", got)
	}

	t.Setenv("RATE_LIMIT_LOGIN_REQUESTS", "7")
	t.Setenv("RATE_LIMIT_LOGIN_WINDOW", "30s")
	if got := rateLimit("LOGIN", 10, time.Minute); got != (RateLimit{Requests: 7, Window: 30 * time.Second}) {
		t.Fatalf("configured rate limit = %+v", got)
	}
}

func TestCSVValues(t *testing.T) {
	want := []string{"127.0.0.1/32", "172.20.0.0/16"}
	if got := csvValues(" 127.0.0.1/32, ,172.20.0.0/16 "); !reflect.DeepEqual(got, want) {
		t.Fatalf("values=%v want=%v", got, want)
	}
}
