package models

type User struct {
	BaseModel
	FirstName string `gorm:"not null"`
	LastName  string `gorm:"not null"`

	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	AuthVersion  int64  `gorm:"not null;default:0;check:auth_version >= 0"`
}
