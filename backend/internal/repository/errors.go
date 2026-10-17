package repository

import "github.com/kacperkg/vellum/internal/apperrors"

// ErrNotFound is kept as a package-level alias for compatibility. Repository
// implementations return the shared application sentinel so callers can map
// errors without depending on persistence details.
var ErrNotFound = apperrors.ErrNotFound

var ErrConflict = apperrors.ErrConflict
