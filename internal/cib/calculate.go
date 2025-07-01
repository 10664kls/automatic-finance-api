package cib

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/database"
	"github.com/10664kls/automatic-finance-api/internal/gemini"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

var ErrCalculationNotFound = errors.New("calculation not found")

type Calculation struct {
	ID                    int64                 `json:"id"`
	CIBFileName           string                `json:"cibFileName"`
	Number                string                `json:"number"`
	Customer              Customer              `json:"customer"`
	TotalInstallmentInLAK decimal.Decimal       `json:"totalInstallmentInLAK"`
	AggregateQuantity     AggregateQuantity     `json:"aggregateQuantity"`
	AggregateByBankCode   []AggregateByBankCode `json:"aggregateByBankCode"`
	Contracts             []Contract            `json:"contracts"`
	CreatedBy             string                `json:"createdBy"`
	UpdatedBy             string                `json:"updatedBy"`
	CreatedAt             time.Time             `json:"createdAt"`
	UpdatedAt             time.Time             `json:"updatedAt"`
}

func (c Calculation) BytesFromContracts() []byte {
	bytes, _ := json.Marshal(c.Contracts)
	return bytes
}

func (c Calculation) BytesFromAggregateByBankCode() []byte {
	bytes, _ := json.Marshal(c.AggregateByBankCode)
	return bytes
}

type Customer struct {
	DisplayName string   `json:"displayName"`
	PhoneNumber string   `json:"phoneNumber"`
	DateOfBirth yyyymmdd `json:"dateOfBirth"`
}

type AggregateQuantity struct {
	Total  decimal.Decimal `json:"total"`
	Closed decimal.Decimal `json:"closed"`
	Active decimal.Decimal `json:"active"`
}

type AggregateByBankCode struct {
	BankCode string          `json:"bankCode"`
	Quantity decimal.Decimal `json:"quantity"`
}

type Contract struct {
	Number               string   `json:"number"`
	BankCode             string   `json:"bankCode"`
	Type                 string   `json:"type"`
	Currency             string   `json:"currency"`
	GradeCIB             string   `json:"gradeCIB"`
	Term                 string   `json:"term"`
	GradeCIBLast12Months []string `json:"gradeCIBLast12months"`
	Status               status   `json:"status"`

	// termType used for calculate
	termType termType
	TermType string `json:"termType"`

	LastedAt           yyyymmdd        `json:"lastedAt"`
	FirstInstallment   yyyymmdd        `json:"firstInstallment"`
	LastInstallment    yyyymmdd        `json:"lastInstallment"`
	InterestRate       decimal.Decimal `json:"interestRate"`
	FinanceAmount      decimal.Decimal `json:"financeAmount"`
	OutstandingBalance decimal.Decimal `json:"outstandingBalance"`
	OverdueInDay       decimal.Decimal `json:"overdueInDay"`
	Period             decimal.Decimal `json:"period"`
	Installment        decimal.Decimal `json:"installment"`
	InstallmentInLAK   decimal.Decimal `json:"installmentInLAK"`
	ExchangeRate       decimal.Decimal `json:"exchangeRate"`
}

type CalculateReq struct {
	fileID      int64
	Number      string `json:"number"`
	CIBFileName string `json:"cibFileName"`
}

func (r *CalculateReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.CIBFileName == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "cibFileName",
			Description: "CIB file name must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

func newCalculationFromCIBInfo(by string, number string, fileName string, cib *gemini.CIBInfo, currencies []*currency.Currency) *Calculation {
	now := time.Now()
	c := new(Calculation)
	c.CreatedBy = by
	c.UpdatedBy = by
	c.CreatedAt = now
	c.UpdatedAt = now
	c.Number = number
	c.CIBFileName = fileName
	c.Customer.DisplayName = cib.Account.DisplayName
	c.Customer.PhoneNumber = cib.Account.PhoneNumber
	c.Contracts = newContracts(cib.Contracts, currenciesToMap(currencies))
	c.AggregateQuantity = newAggregateQuantity(c.Contracts)
	c.AggregateByBankCode = newAggregateByBankCodes(cib.AggregateByBankCodes)
	c.TotalInstallmentInLAK = sumInstallment(c.Contracts)
	if d, err := ParseDDMMYYYY("02/01/2006", cib.Account.DateOfBirth); err == nil {
		c.Customer.DateOfBirth = d
	}

	return c
}

func currenciesToMap(currencies []*currency.Currency) map[string]decimal.Decimal {
	m := make(map[string]decimal.Decimal)
	for _, c := range currencies {
		m[c.Code] = c.ExchangeRate
	}
	return m
}

func newAggregateQuantity(contracts []Contract) AggregateQuantity {
	a := AggregateQuantity{
		Total:  decimal.Zero,
		Closed: decimal.Zero,
		Active: decimal.Zero,
	}

	for _, c := range contracts {
		a.Total = a.Total.Add(decimal.NewFromInt(1))
		switch c.Status {
		case StatusActive:
			a.Active = a.Active.Add(decimal.NewFromInt(1))

		case StatusClosed:
			a.Closed = a.Closed.Add(decimal.NewFromInt(1))
		}
	}

	return a
}

func newAggregateByBankCodes(args []gemini.AggregateByBankCode) []AggregateByBankCode {
	bs := make([]AggregateByBankCode, 0)

	for _, b := range args {
		bs = append(bs, AggregateByBankCode{
			BankCode: b.BankCode,
			Quantity: decimal.NewFromInt(b.Quantity),
		})
	}

	return bs
}

func newContract(contract gemini.Contract, exchangeRate decimal.Decimal) Contract {
	var c Contract
	startedAt, err := ParseDDMMYYYY("02-01-2006", contract.FirstInstallmentDate)
	if err == nil {
		c.FirstInstallment = startedAt
	}

	endedAt, err := ParseDDMMYYYY("02-01-2006", contract.LastInstallmentDate)
	if err == nil {
		c.LastInstallment = endedAt
	}

	lastedAt, err := ParseDDMMYYYY("02-01-2006", contract.LastedAt)
	if err == nil {
		c.LastedAt = lastedAt
	}

	c.Number = contract.LoanNumber
	c.TermType = contract.TermType
	c.termType = termTypeFromTypeOfTermLoan(contract.TermType)
	c.Status = statusFromContractStatus(contract.Status)
	c.Period = countMonth(startedAt.Time(), endedAt.Time())
	c.BankCode = contract.BankCode
	c.Currency = contract.Currency
	c.GradeCIB = contract.GradeCIB
	c.ExchangeRate = exchangeRate
	c.OverdueInDay = contract.OverdueDay
	c.GradeCIBLast12Months = contract.GradeCIBLast12Months
	c.InterestRate = contract.InterestRate
	c.Term = contract.Term
	c.Type = contract.Type
	c.OutstandingBalance = contract.OutstandingBalance
	c.FinanceAmount = contract.FinanceAmount

	installment := calculateInstallment(c)
	c.Installment = installment
	c.InstallmentInLAK = convertToLAK(installment, exchangeRate)

	return c
}

func newContracts(contracts []gemini.Contract, currencies map[string]decimal.Decimal) []Contract {
	cs := make([]Contract, len(contracts))

	for i, c := range contracts {
		exchangeRate, ok := currencies[c.Currency]
		if !ok {
			exchangeRate = decimal.NewFromInt(1)
		}

		cs[i] = newContract(c, exchangeRate)
	}
	return cs
}

func calculateInstallment(c Contract) decimal.Decimal {
	if c.Status != StatusActive {
		return decimal.Zero
	}

	switch c.termType {
	case TermTypeCL:
		return calculatePrincipalPlusFlatInterestPayment(c.FinanceAmount, c.InterestRate, c.Period)

	case TermTypeL, TermTypePL:
		return calculatePMT(c.InterestRate, c.Period, c.FinanceAmount)

	case TermTypeOD, TermTypeCC, TermTypeRL:
		return calculateTenPercentOfMonthlyAccruedBalance(c.OutstandingBalance, c.InterestRate)

	case TermTypeOther:
		return calculatePMT(c.InterestRate, c.Period, c.FinanceAmount)
	}

	return decimal.Zero
}

func sumInstallment(contracts []Contract) decimal.Decimal {
	var total decimal.Decimal
	for _, c := range contracts {
		if strings.ToUpper(c.BankCode) == "KLS_LS" {
			continue
		}

		total = total.Add(c.InstallmentInLAK)
	}

	return total
}

func calculateTenPercentOfMonthlyAccruedBalance(outstandingBalance, interest decimal.Decimal) decimal.Decimal {
	if interest.IsZero() || outstandingBalance.IsZero() {
		return decimal.Zero
	}

	hundred := decimal.NewFromInt(100)
	twelve := decimal.NewFromInt(12)
	tenPercent := decimal.NewFromFloat(0.10) // 10% as 0.10

	monthlyRate := interest.Div(hundred).Div(twelve)
	growthFactor := decimal.NewFromInt(1).Add(monthlyRate)
	amountAfterInterest := outstandingBalance.Mul(growthFactor)

	return amountAfterInterest.Mul(tenPercent)
}

func calculatePrincipalPlusFlatInterestPayment(financeAmount decimal.Decimal, interest decimal.Decimal, period decimal.Decimal) decimal.Decimal {
	if period.IsZero() || financeAmount.IsZero() || interest.IsZero() {
		return decimal.Zero
	}

	hundred := decimal.NewFromInt(100)
	rate := financeAmount.Mul(interest.Div(hundred))
	return financeAmount.Div(period).Add(rate)
}

func calculatePMT(interest decimal.Decimal, period decimal.Decimal, financeAmount decimal.Decimal) decimal.Decimal {
	if period.IsZero() || financeAmount.IsZero() {
		return decimal.Zero
	}

	hundred := decimal.NewFromInt(100)
	twelve := decimal.NewFromInt(12)
	monthlyRate := interest.Div(hundred).Div(twelve)

	if monthlyRate.IsZero() {
		return financeAmount.Div(period)
	}

	onePlusRate := decimal.NewFromInt(1).Add(monthlyRate)
	exponent := decimal.NewFromFloat(-period.InexactFloat64())
	pow := onePlusRate.Pow(exponent)
	denominator := decimal.NewFromInt(1).Sub(pow)

	if denominator.IsZero() {
		return decimal.Zero
	}

	numerator := monthlyRate.Mul(financeAmount)
	return numerator.Div(denominator)
}

func countMonth(from, to time.Time) decimal.Decimal {
	if to.Before(from) {
		return decimal.Zero
	}

	yearDiff := to.Year() - from.Year()
	monthDiff := int(to.Month()) - int(from.Month())

	return decimal.NewFromInt(int64(yearDiff*12 + monthDiff))
}

func termTypeFromTypeOfTermLoan(t string) termType {
	t = strings.TrimSpace(t)
	t = strings.ToUpper(t)
	switch t {
	case "CL":
		return TermTypeCL

	case "L", "PL":
		return TermTypeL

	case "OD", "CC", "RL":
		return TermTypeOD

	default:
		return TermTypeOther
	}
}
func statusFromContractStatus(status string) status {
	switch strings.TrimSpace(status) {
	case "ເຄື່ອນໄຫວ":
		return StatusActive

	case "ບໍ່ເຄື່ອນໄຫວ/ປິດບັນຊີ":
		return StatusClosed

	}
	return StatusUnSpecified
}

func convertToLAK(amount decimal.Decimal, exchangeRate decimal.Decimal) decimal.Decimal {
	return amount.Mul(exchangeRate)
}

func isCalculationExists(ctx context.Context, db *sql.DB, number string) (bool, error) {
	q, args := sq.Select("TOP 1 number").
		From("cib_file_analysis").
		Where(sq.Eq{
			"number": number,
		}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	row := db.QueryRowContext(ctx, q, args...)
	var n string
	err := row.Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // No calculation found
	}
	if err != nil {
		return false, fmt.Errorf("failed to check if calculation exists: %w", err)
	}

	return n != "", nil // Calculation exists if number is not empty
}

func saveCalculation(ctx context.Context, db *sql.DB, in *Calculation) error {
	return database.WithTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		updatedQuery, args := sq.Update("cib_file_analysis").
			Set("number", in.Number).
			Set("cib_file_name", in.CIBFileName).
			Set("customer_display_name", in.Customer.DisplayName).
			Set("customer_phone_number", in.Customer.PhoneNumber).
			Set("customer_dob", in.Customer.DateOfBirth).
			Set("total_loan", in.AggregateQuantity.Total).
			Set("total_closed_loan", in.AggregateQuantity.Closed).
			Set("total_active_loan", in.AggregateQuantity.Active).
			Set("total_installment_lak", in.TotalInstallmentInLAK).
			Set("contract_info", in.BytesFromContracts()).
			Set("aggregate_by_bank", in.BytesFromAggregateByBankCode()).
			Set("updated_by", in.UpdatedBy).
			Set("updated_at", in.UpdatedAt).
			Where(sq.Eq{
				"number": in.Number,
			}).
			PlaceholderFormat(sq.AtP).
			MustSql()

		effected, err := tx.ExecContext(ctx, updatedQuery, args...)
		if err != nil {
			return fmt.Errorf("failed to update calculation: %w", err)
		}

		rowsEffected, err := effected.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsEffected > 0 {
			return nil
		}

		insertQuery, args := sq.Insert("cib_file_analysis").
			Columns(
				"number",
				"cib_file_name",
				"customer_display_name",
				"customer_phone_number",
				"customer_dob",
				"total_loan",
				"total_closed_loan",
				"total_active_loan",
				"total_installment_lak",
				"aggregate_by_bank",
				"contract_info",
				"created_by",
				"created_at",
				"updated_by",
				"updated_at",
			).
			Values(
				in.Number,
				in.CIBFileName,
				in.Customer.DisplayName,
				in.Customer.PhoneNumber,
				in.Customer.DateOfBirth,
				in.AggregateQuantity.Total,
				in.AggregateQuantity.Closed,
				in.AggregateQuantity.Active,
				in.TotalInstallmentInLAK,
				in.BytesFromAggregateByBankCode(),
				in.BytesFromContracts(),
				in.CreatedBy,
				in.CreatedAt,
				in.UpdatedBy,
				in.UpdatedAt,
			).
			Suffix("SELECT SCOPE_IDENTITY()").
			PlaceholderFormat(sq.AtP).
			MustSql()

		row := tx.QueryRowContext(ctx, insertQuery, args...)
		if err := row.Scan(&in.ID); err != nil {
			return fmt.Errorf("failed to insert calculation: %w", err)
		}

		return nil
	})
}

type CalculationQuery struct {
	ID                  int64     `query:"id"`
	Number              string    `query:"number"`
	CustomerDisplayName string    `query:"customerDisplayName"`
	CreatedAfter        time.Time `query:"createdAfter"`
	CreatedBefore       time.Time `query:"createdBefore"`
	PageSize            uint64    `query:"pageSize"`
	PageToken           string    `query:"pageToken"`
}

func (q *CalculationQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != 0 {
		and = append(and, sq.Eq{"id": q.ID})
	}
	if q.Number != "" {
		and = append(and, sq.Eq{"number": q.Number})
	}
	if q.CustomerDisplayName != "" {
		and = append(and, sq.Expr("customer_display_name LIKE ?", "%"+q.CustomerDisplayName+"%"))
	}

	if !q.CreatedAfter.IsZero() {
		and = append(and, sq.GtOrEq{"created_at": q.CreatedAfter})
	}

	if !q.CreatedBefore.IsZero() {
		and = append(and, sq.LtOrEq{"created_at": q.CreatedBefore})
	}

	if q.PageToken != "" {
		cursor, err := pager.DecodeCursor(q.PageToken)
		if err == nil {
			and = append(and, sq.Lt{"created_at": cursor.Time})
		}
	}

	return and.ToSql()
}
func listCalculations(ctx context.Context, db *sql.DB, in *CalculationQuery) ([]*Calculation, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))

	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.
		Select(
			id,
			"number",
			"cib_file_name",
			"customer_display_name",
			"customer_phone_number",
			"customer_dob",
			"total_loan",
			"total_closed_loan",
			"total_active_loan",
			"total_installment_lak",
			"aggregate_by_bank",
			"contract_info",
			"created_by",
			"created_at",
			"updated_by",
			"updated_at",
		).
		From(`cib_file_analysis`).
		Where(pred, args...).
		PlaceholderFormat(sq.AtP).
		OrderBy("created_at DESC").
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for listing calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]*Calculation, 0)
	for rows.Next() {
		var c Calculation
		var contracts, aggregateBank []byte
		err := rows.Scan(
			&c.ID,
			&c.Number,
			&c.CIBFileName,
			&c.Customer.DisplayName,
			&c.Customer.PhoneNumber,
			&c.Customer.DateOfBirth,
			&c.AggregateQuantity.Total,
			&c.AggregateQuantity.Closed,
			&c.AggregateQuantity.Active,
			&c.TotalInstallmentInLAK,
			&aggregateBank,
			&contracts,
			&c.CreatedBy,
			&c.CreatedAt,
			&c.UpdatedBy,
			&c.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCalculationNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan calculation: %w", err)
		}

		if err := json.Unmarshal(contracts, &c.Contracts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contracts: %w", err)
		}

		banks := make([]AggregateByBankCode, 0)
		if err := json.Unmarshal(aggregateBank, &banks); err != nil {
			return nil, fmt.Errorf("failed to unmarshal aggregate by bank: %w", err)
		}

		c.AggregateByBankCode = banks
		calculations = append(calculations, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate calculations: %w", err)
	}

	return calculations, nil
}

func getCalculation(ctx context.Context, db *sql.DB, in *CalculationQuery) (*Calculation, error) {
	in.PageSize = 1

	if in.ID == 0 && in.Number == "" {
		return nil, ErrCalculationNotFound
	}

	calculations, err := listCalculations(ctx, db, in)
	if err != nil {
		return nil, err
	}
	if len(calculations) == 0 {
		return nil, ErrCalculationNotFound
	}

	return calculations[0], nil
}
