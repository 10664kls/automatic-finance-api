package types

import (
	"database/sql/driver"
	"fmt"
	"strconv"
)

type ProductType int

const (
	ProductUnSpecified ProductType = iota
	ProductSA
	ProductSF
	ProductPL
)

var productNames = map[ProductType]string{
	ProductUnSpecified: "UNSPECIFIED",
	ProductSA:          "SA",
	ProductSF:          "SF",
	ProductPL:          "PL",
}

var productValues = map[string]ProductType{
	"UNSPECIFIED": ProductUnSpecified,
	"SA":          ProductSA,
	"SF":          ProductSF,
	"PL":          ProductPL,
}

func (p ProductType) String() string {
	if v, ok := productNames[p]; ok {
		return v
	}
	return fmt.Sprintf("Product(%d)", p)
}

func (p ProductType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + p.String() + `"`), nil
}

func (p *ProductType) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	if t, ok := productValues[string(b)]; ok {
		*p = t
		return nil
	}

	if t, err := strconv.Atoi(string(b)); err == nil {
		*p = ProductType(t)
		return nil
	}

	return fmt.Errorf("invalid product: %s", string(b))
}

func (p *ProductType) Scan(src any) error {
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

func (p ProductType) Value() (driver.Value, error) {
	return p.String(), nil
}
