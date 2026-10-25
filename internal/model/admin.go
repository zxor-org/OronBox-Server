package model

import (
	"time"
)

type AdminSession struct {
	ID                string
	UserID            string
	Username          string
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	RotatedAt         time.Time
	Generation        int64
}
