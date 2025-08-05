package income

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

type source int

const (
	SourceUnSpecified source = iota
	SourceSalary
	SourceAllowance
	SourceCommission
	SourceBasicSalaryInterview
)

var sourceNames = map[source]string{
	SourceUnSpecified:          "UNSPECIFIED",
	SourceSalary:               "SALARY",
	SourceAllowance:            "ALLOWANCE",
	SourceCommission:           "COMMISSION",
	SourceBasicSalaryInterview: "BASIC_SALARY_INTERVIEW",
}

var sourceValues = map[string]source{
	"UNSPECIFIED":            SourceUnSpecified,
	"SALARY":                 SourceSalary,
	"ALLOWANCE":              SourceAllowance,
	"COMMISSION":             SourceCommission,
	"BASIC_SALARY_INTERVIEW": SourceBasicSalaryInterview,
}

func (s source) String() string {
	if v, ok := sourceNames[s]; ok {
		return v
	}
	return fmt.Sprintf("Source(%d)", s)
}

func (s source) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

func (s *source) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if t, err := strconv.Atoi(string(b)); err == nil {
		*s = source(t)
		return nil
	}

	if t, ok := sourceValues[string(b)]; ok {
		*s = t
		return nil
	}

	return fmt.Errorf("invalid source: %s", string(b))
}

func (s *source) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		if v, ok := sourceValues[src]; ok {
			*s = v
			return nil
		}

	case []byte:
		if v, ok := sourceValues[string(src)]; ok {
			*s = v
			return nil
		}
	}

	return fmt.Errorf("invalid source: %v", src)
}

func (s source) Value() (driver.Value, error) {
	return s.String(), nil
}
