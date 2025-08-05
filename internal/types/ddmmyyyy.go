package types

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type MMYYY time.Time

func (y MMYYY) String() string {
	return time.Time(y).Format("January-2006")
}

func (y MMYYY) MarshalJSON() ([]byte, error) {
	return []byte(`"` + y.String() + `"`), nil
}

func (y *MMYYY) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	t, err := time.Parse(`January-2006`, string(b))
	if err != nil {
		return err
	}
	*y = MMYYY(t)
	return nil
}

func (y MMYYY) Value() (driver.Value, error) {
	return y.String(), nil
}

func (y *MMYYY) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		t, err := time.Parse(`January-2006`, src)
		if err != nil {
			return err
		}
		*y = MMYYY(t)
		return nil

	case []byte:
		t, err := time.Parse(`January-2006`, string(src))
		if err != nil {
			return err
		}
		*y = MMYYY(t)
		return nil

	case time.Time:
		*y = MMYYY(src)
		return nil
	}

	return fmt.Errorf("invalid yyyymm: %v", src)
}

func (y MMYYY) Time() time.Time {
	return time.Time(y)
}

type DDMMYYYY time.Time

func (y DDMMYYYY) String() string {
	return time.Time(y).Format("02-01-2006")
}

func (y DDMMYYYY) MarshalJSON() ([]byte, error) {
	return []byte(`"` + y.String() + `"`), nil
}

func (y *DDMMYYYY) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}

	b = b[1 : len(b)-1]
	t, err := time.Parse(`02-01-2006`, string(b))
	if err != nil {
		return err
	}
	*y = DDMMYYYY(t)
	return nil
}

func (y DDMMYYYY) Value() (driver.Value, error) {
	return y.String(), nil
}

func (y *DDMMYYYY) Scan(src any) error {
	if src == nil {
		return nil
	}

	switch src := src.(type) {
	case string:
		t, err := time.Parse(`02-01-2006`, src)
		if err != nil {
			return err
		}
		*y = DDMMYYYY(t)
		return nil

	case []byte:
		t, err := time.Parse(`02-01-2006`, string(src))
		if err != nil {
			return err
		}
		*y = DDMMYYYY(t)
		return nil

	case time.Time:
		*y = DDMMYYYY(src)
		return nil

	}

	return fmt.Errorf("invalid yyyymm: %v", src)
}

func (y DDMMYYYY) Time() time.Time {
	return time.Time(y)
}

func ParseYYYYMM(layout, s string) (MMYYY, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return MMYYY{}, err
	}

	return MMYYY(t), nil
}

func ParseDDMMYYYY(layout, s string) (DDMMYYYY, error) {
	t, err := time.Parse(layout, s)
	if err != nil {
		return DDMMYYYY{}, err
	}

	return DDMMYYYY(t), nil
}
