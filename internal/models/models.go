package models

import "time"

// Click is a unique visit of a user to an author's content during a UTC day.
type Click struct {
	UserID   int64
	AuthorID int64
	Date     time.Time
}
