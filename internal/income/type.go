package income

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"
)

type product int

const (
	ProductUnSpecified product = iota
	ProductSA
	ProductSF
	ProductPL
)

var productNames = map[product]string{
	ProductUnSpecified: "UNSPECIFIED",
	ProductSA:          "SA",
	ProductSF:          "SF",
	ProductPL:          "PL",
}

var productValues = map[string]product{
	"UNSPECIFIED": ProductUnSpecified,
	"SA":          ProductSA,
	"SF":          ProductSF,
	"PL":          ProductPL,
}

func (p product) String() string {
	if v, ok := productNames[p]; ok {
		return v
	}
	return fmt.Sprintf("Product(%d)", p)
}

func (p product) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.String() + `"`), nil
}

func (p *product) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if t, ok := productValues[string(b)]; ok {
		*p = t
		return nil
	}

	if t, err := strconv.Atoi(string(b)); err == nil {
		*p = product(t)
		return nil
	}

	return fmt.Errorf("invalid product: %s", string(b))
}

func (p *product) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		if v, ok := productValues[src]; ok {
			*p = v
			return nil
		}

	case []byte:
		if v, ok := productValues[string(src)]; ok {
			*p = v
			return nil
		}
	}

	return fmt.Errorf("invalid product: %v", src)
}

func (p product) Value() (driver.Value, error) {
	return p.String(), nil
}

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

type mmyyyy time.Time

func (y mmyyyy) String() string {
	return time.Time(y).Format("January-2006")
}

func (y mmyyyy) MarshalJSON() ([]byte, error) {
	return []byte(`"` + y.String() + `"`), nil
}

func (y *mmyyyy) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	t, err := time.Parse(`January-2006`, string(b))
	if err != nil {
		return err
	}
	*y = mmyyyy(t)
	return nil
}

func (y mmyyyy) Value() (driver.Value, error) {
	return y.String(), nil
}

func (y *mmyyyy) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		t, err := time.Parse(`January-2006`, src)
		if err != nil {
			return err
		}
		*y = mmyyyy(t)
		return nil

	case []byte:
		t, err := time.Parse(`January-2006`, string(src))
		if err != nil {
			return err
		}
		*y = mmyyyy(t)
		return nil

	case time.Time:
		*y = mmyyyy(src)
		return nil
	}

	return fmt.Errorf("invalid yyyymm: %v", src)
}

func (y mmyyyy) Time() time.Time {
	return time.Time(y)
}

type ddmmyyyy time.Time

func (y ddmmyyyy) String() string {
	return time.Time(y).Format("02-01-2006")
}

func (y ddmmyyyy) MarshalJSON() ([]byte, error) {
	return []byte(`"` + y.String() + `"`), nil
}

func (y *ddmmyyyy) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	t, err := time.Parse(`02-01-2006`, string(b))
	if err != nil {
		return err
	}
	*y = ddmmyyyy(t)
	return nil
}

func (y ddmmyyyy) Value() (driver.Value, error) {
	return y.String(), nil
}

func (y *ddmmyyyy) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		t, err := time.Parse(`02-01-2006`, src)
		if err != nil {
			return err
		}
		*y = ddmmyyyy(t)
		return nil

	case []byte:
		t, err := time.Parse(`02-01-2006`, string(src))
		if err != nil {
			return err
		}
		*y = ddmmyyyy(t)
		return nil

	case time.Time:
		*y = ddmmyyyy(src)
		return nil

	}

	return fmt.Errorf("invalid yyyymm: %v", src)
}

func (y ddmmyyyy) Time() time.Time {
	return time.Time(y)
}

func ParseYYYYMM(layout, s string) (mmyyyy, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return mmyyyy{}, err
	}

	return mmyyyy(t), nil
}

func ParseDDMMYYYY(layout, s string) (ddmmyyyy, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return ddmmyyyy{}, err
	}

	return ddmmyyyy(t), nil
}

type status int

const (
	StatusUnSpecified status = iota
	StatusPending
	StatusCompleted
)

var statusNames = map[status]string{
	StatusUnSpecified: "UNSPECIFIED",
	StatusPending:     "PENDING",
	StatusCompleted:   "COMPLETED",
}

var statusValues = map[string]status{
	"UNSPECIFIED": StatusUnSpecified,
	"PENDING":     StatusPending,
	"COMPLETED":   StatusCompleted,
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

func (s status) Value() (driver.Value, error) {
	return s.String(), nil
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
