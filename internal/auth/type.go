package auth

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

type status int

const (
	StatusUnSpecified status = iota
	StatusDisabled
	StatusClosed
	StatusEnabled
)

var statusNames = map[status]string{
	StatusUnSpecified: "UNSPECIFIED",
	StatusEnabled:     "ENABLED",
	StatusDisabled:    "DISABLED",
	StatusClosed:      "CLOSED",
}

var statusValues = map[string]status{
	"UNSPECIFIED": StatusUnSpecified,
	"ENABLED":     StatusEnabled,
	"DISABLED":    StatusDisabled,
	"CLOSED":      StatusClosed,
}

func (s status) String() string {
	if v, ok := statusNames[s]; ok {
		return v
	}
	return fmt.Sprintf("Status(%d)", s)
}

func (s status) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *status) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if v, ok := statusValues[string(b)]; ok {
		*s = v
		return nil
	}

	if v, err := strconv.Atoi(string(b)); err == nil {
		*s = status(v)
		return nil
	}

	return fmt.Errorf("invalid status: %s", string(b))
}

func (s *status) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		if v, ok := statusValues[src]; ok {
			*s = v
			return nil
		}

	case []byte:
		if v, ok := statusValues[string(src)]; ok {
			*s = v
			return nil
		}
	}

	return fmt.Errorf("invalid status: %v", src)
}

func (s status) Value() (driver.Value, error) {
	return s.String(), nil
}
