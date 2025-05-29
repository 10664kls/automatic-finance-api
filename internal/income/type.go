package income

import (
	"database/sql/driver"
	"fmt"
	"strconv"
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
)

var sourceNames = map[source]string{
	SourceUnSpecified: "UNSPECIFIED",
	SourceSalary:      "SALARY",
	SourceAllowance:   "ALLOWANCE",
	SourceCommission:  "COMMISSION",
}

var sourceValues = map[string]source{
	"UNSPECIFIED": SourceUnSpecified,
	"SALARY":      SourceSalary,
	"ALLOWANCE":   SourceAllowance,
	"COMMISSION":  SourceCommission,
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
	if t, ok := sourceValues[string(b)]; ok {
		*s = t
		return nil
	}

	if t, err := strconv.Atoi(string(b)); err == nil {
		*s = source(t)
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
