package config

import (
	"reflect"
	"testing"
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
