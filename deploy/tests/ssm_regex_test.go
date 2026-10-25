package ssmtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// AWS validates allowedPattern using Go regexp as well as Java. Python's
// regexp engine accepts oversized counted repetitions, so it missed this bug.
func TestManifestParameterValidation(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate SSM templates")
	}
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(filepath.Dir(source), "..", "ssm", environment+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Parameters map[string]struct {
					Type              string `json:"type"`
					InterpolationType string `json:"interpolationType"`
					AllowedPattern    string `json:"allowedPattern"`
					MinChars          int    `json:"minChars"`
					MaxChars          int    `json:"maxChars"`
				} `json:"parameters"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			parameter, exists := document.Parameters["Manifest"]
			if !exists {
				t.Fatal("missing Manifest parameter")
			}
			pattern, err := regexp.Compile(parameter.AllowedPattern)
			if err != nil {
				t.Fatalf("allowedPattern is incompatible with the SSM Agent Go regex engine: %v", err)
			}
			if parameter.Type != "String" || parameter.InterpolationType != "ENV_VAR" {
				t.Fatal("Manifest must remain a String with safe ENV_VAR interpolation")
			}
			if parameter.MinChars != 1 || parameter.MaxChars != 45000 {
				t.Fatalf("manifest length limits = %d..%d, want 1..45000", parameter.MinChars, parameter.MaxChars)
			}
			for _, test := range []struct {
				name, value string
				want        bool
			}{
				{"base64 JSON", "e30=", true},
				{"base64 alphabet", "AZaz09+/=", true},
				{"minimum length", "A", true},
				{"maximum length", strings.Repeat("A", 45000), true},
				{"over maximum", strings.Repeat("A", 45001), false},
				{"empty", "", false},
				{"newline", "e30=\n", false},
				{"whitespace", "e30= ", false},
				{"shell input", "$(id)", false},
				{"SSM interpolation", "{{Manifest}}", false},
				{"non ASCII", "é", false},
			} {
				t.Run(test.name, func(t *testing.T) {
					// The alphabet regex and native SSM length constraints work
					// together; strict base64/JSON validation remains in Python.
					got := len(test.value) >= parameter.MinChars && len(test.value) <= parameter.MaxChars && pattern.MatchString(test.value)
					if got != test.want {
						t.Fatalf("parameter accepted=%v, want %v", got, test.want)
					}
				})
			}
		})
	}
}
