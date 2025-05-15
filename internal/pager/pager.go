package pager

import (
	"encoding/base64"
	"encoding/json"
	"time"
)

const (
	defaultSize = 20
	maxSize     = 250
)

// Size returns the size of the page.
// If size is less than 1, it returns the 20 as default.
// If size is greater than 250, it returns 250 as max size.
func Size(size uint64) uint64 {
	if size < 1 {
		return defaultSize
	}
	if size > maxSize {
		return maxSize
	}

	return size
}

// Cursor is designed to be used as a pagination cursor for this project only.
type Cursor struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
}

// EncodeCursor encodes the cursor to a string in base64 format.
func EncodeCursor(c *Cursor) string {
	j, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(j)
}

// DecodeCursor decodes the cursor from a base64 string.
func DecodeCursor(s string) (*Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}

	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}
