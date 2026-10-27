package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshToken stores only a hash of the opaque token returned to the client.
// A token is valid while it has not expired and has not been revoked.
type RefreshToken struct {
	BaseModel

	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	AuthVersion int64     `gorm:"not null;default:0"`
	// Snapshot checked under the user lock when a login session is persisted.
	PasswordHashSnapshot string `gorm:"-" json:"-"`

	TokenHash string     `gorm:"size:64;uniqueIndex;not null"`
	ExpiresAt time.Time  `gorm:"not null;index"`
	RevokedAt *time.Time `gorm:"index"`
}
