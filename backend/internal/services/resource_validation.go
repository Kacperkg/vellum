package services

import (
	"time"
	"unicode/utf8"
)

// MaxResourceNameLength bounds account, goal, transaction and transfer names in
// Unicode code points, before trimming. Stored values are never truncated.
const MaxResourceNameLength = 200

// MaxTransactionDateRange bounds an explicitly filtered query. The complete
// collection endpoints retain their existing response contract (Issue #25).
const MaxTransactionDateRange = 366 * 24 * time.Hour

var ErrResourceNameTooLong = invalid("name must be at most 200 characters")

func validateResourceName(name string) error {
	if utf8.RuneCountInString(name) > MaxResourceNameLength {
		return ErrResourceNameTooLong
	}
	return nil
}
