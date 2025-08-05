package types

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

type AnalysisStatus int

const (
	StatusUnSpecified AnalysisStatus = iota
	StatusPending
	StatusCompleted
)

var statusNames = map[AnalysisStatus]string{
	StatusUnSpecified: "UNSPECIFIED",
	StatusPending:     "PENDING",
	StatusCompleted:   "COMPLETED",
}

var statusValues = map[string]AnalysisStatus{
	"UNSPECIFIED": StatusUnSpecified,
	"PENDING":     StatusPending,
	"COMPLETED":   StatusCompleted,
}

func (s AnalysisStatus) String() string {
	if v, ok := statusNames[s]; ok {
		return v
	}
	return fmt.Sprintf("Status(%d)", s)
}

func (s AnalysisStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *AnalysisStatus) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if v, ok := statusValues[string(b)]; ok {
		*s = v
		return nil
	}

	if v, err := strconv.Atoi(string(b)); err == nil {
		*s = AnalysisStatus(v)
		return nil
	}

	return fmt.Errorf("invalid status: %s", string(b))
}

func (s AnalysisStatus) Value() (driver.Value, error) {
	return s.String(), nil
}

func (s *AnalysisStatus) Scan(src any) error {
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
