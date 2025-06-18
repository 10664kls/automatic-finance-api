package cib

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type yyyymmdd time.Time

func (y yyyymmdd) String() string {
	return time.Time(y).Format("2006-01-02")
}

func (y yyyymmdd) MarshalJSON() ([]byte, error) {
	return []byte(`"` + y.String() + `"`), nil
}

func (y *yyyymmdd) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	t, err := time.Parse(`2006-01-02`, string(b))
	if err == nil {
		*y = yyyymmdd(t)
		return nil
	}

	t, err = time.Parse(`02/01/2006`, string(b))
	if err == nil {
		*y = yyyymmdd(t)
		return nil
	}

	return errors.New("invalid yyyymmdd")
}

func (y yyyymmdd) Value() (driver.Value, error) {
	return y.String(), nil
}

func (y *yyyymmdd) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		t, err := time.Parse(`2006-01-02`, src)
		if err != nil {
			return err
		}
		*y = yyyymmdd(t)
		return nil

	case []byte:
		t, err := time.Parse(`2006-01-02`, string(src))
		if err != nil {
			return err
		}
		*y = yyyymmdd(t)
		return nil

	case time.Time:
		*y = yyyymmdd(src)
		return nil
	}

	return fmt.Errorf("invalid yyyymmdd: %v", src)
}

func (y yyyymmdd) Time() time.Time {
	return time.Time(y)
}

func ParseDDMMYYYY(layout, s string) (yyyymmdd, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return yyyymmdd{}, err
	}

	return yyyymmdd(t), nil
}

type termType int

const (
	TermTypeUnSpecified termType = iota
	TermTypeCL
	TermTypeL
	TermTypePL
	TermTypeOD
	TermTypeCC
	TermTypeRL
	TermTypeOther
)

var termTypeNames = map[termType]string{
	TermTypeUnSpecified: "UNSPECIFIED",
	TermTypeCL:          "CL",
	TermTypeL:           "L",
	TermTypePL:          "PL",
	TermTypeOD:          "OD",
	TermTypeCC:          "CC",
	TermTypeRL:          "RL",
	TermTypeOther:       "OTHER",
}

var termTypeValues = map[string]termType{
	"UNSPECIFIED": TermTypeUnSpecified,
	"CL":          TermTypeCL,
	"L":           TermTypeL,
	"PL":          TermTypePL,
	"OD":          TermTypeOD,
	"CC":          TermTypeCC,
	"RL":          TermTypeRL,
	"OTHER":       TermTypeOther,
}

func (p termType) String() string {
	if v, ok := termTypeNames[p]; ok {
		return v
	}
	return fmt.Sprintf("Term(%d)", p)
}

func (p termType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.String() + `"`), nil
}

func (p *termType) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if t, err := strconv.Atoi(string(b)); err == nil {
		*p = termType(t)
		return nil
	}

	if t, ok := termTypeValues[string(b)]; ok {
		*p = t
		return nil
	}

	return fmt.Errorf("invalid term type: %s", string(b))
}

func (p *termType) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		if v, ok := termTypeValues[src]; ok {
			*p = v
			return nil
		}

	case []byte:
		if v, ok := termTypeValues[string(src)]; ok {
			*p = v
			return nil
		}
	}

	return fmt.Errorf("invalid term type: %v", src)
}

func (s termType) Value() (driver.Value, error) {
	return s.String(), nil
}

type status int

const (
	StatusUnSpecified status = iota
	StatusActive
	StatusClosed
)

var statusNames = map[status]string{
	StatusUnSpecified: "UNSPECIFIED",
	StatusActive:      "ACTIVE",
	StatusClosed:      "CLOSED",
}

var statusValues = map[string]status{
	"UNSPECIFIED": StatusUnSpecified,
	"ACTIVE":      StatusActive,
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
	if t, err := strconv.Atoi(string(b)); err == nil {
		*s = status(t)
		return nil
	}

	if t, ok := statusValues[string(b)]; ok {
		*s = t
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
