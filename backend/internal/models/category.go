package models

type Category struct {
	BaseModel
	Name string `gorm:"uniqueIndex;not null"`
}
