package store

import (
	"time"
)

// Meta describes a workspace for listing.
type Meta struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReportStore writes report artifacts.
type ReportStore interface {
	WriteReport(workDir, name string, data []byte) error
}
