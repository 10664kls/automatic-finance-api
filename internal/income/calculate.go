package income

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/database"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

// ErrCalculationNotFound is returned when a calculation is not found in the database.
var ErrCalculationNotFound = fmt.Errorf("calculation not found")

type Calculation struct {
	ID                   int64           `json:"id"`
	StatementFileName    string          `json:"statementFileName"`
	Number               string          `json:"number"`
	Product              product         `json:"product"`
	Account              Account         `json:"account"`
	ExchangeRate         decimal.Decimal `json:"exchangeRate"`
	MonthlyAverageIncome decimal.Decimal `json:"monthlyAverageIncome"`
	MonthlyNetIncome     decimal.Decimal `json:"monthlyNetIncome"`
	TotalOtherIncome     decimal.Decimal `json:"totalOtherIncome"`
	TotalBasicSalary     decimal.Decimal `json:"totalBasicSalary"`
	TotalIncome          decimal.Decimal `json:"totalIncome"`
	PeriodInMonth        decimal.Decimal `json:"periodInMonth"`
	StartedAt            time.Time       `json:"startedAt"`
	EndedAt              time.Time       `json:"endedAt"`
	Status               status          `json:"status"`
	CreatedBy            string          `json:"createdBy"`
	UpdatedBy            string          `json:"updatedBy"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`

	SalaryBreakdown     *SalaryBreakdown     `json:"salaryBreakdown"`
	AllowanceBreakdown  *AllowanceBreakdown  `json:"allowanceBreakdown"`
	CommissionBreakdown *CommissionBreakdown `json:"commissionBreakdown"`
	Source              *Source              `json:"source"`
}

func (c *Calculation) ReCalculate(by string, in *RecalculateReq) error {
	c.SalaryBreakdown = newSalaryBreakdown(in.MonthlySalaries)
	c.AllowanceBreakdown = newAllowanceBreakdown(in.Allowances)
	c.CommissionBreakdown = newCommissionBreakdown(in.Commissions)

	mapCal, err := c.toStateMap()
	if err != nil {
		return fmt.Errorf("failed to convert calculation to state map: %w", err)
	}

	c.UpdatedAt = time.Now()
	c.UpdatedBy = by
	c.populate(c.Product, c.PeriodInMonth, c.ExchangeRate, mapCal)
	return nil
}

func (c *Calculation) Complete(by string) {
	c.Status = StatusCompleted
	c.UpdatedAt = time.Now()
	c.UpdatedBy = by
}

func (c *Calculation) IsCompleted() bool {
	return c.Status == StatusCompleted
}

func newSalaryBreakdown(months []MonthlySalary) *SalaryBreakdown {
	return &SalaryBreakdown{
		MonthlySalaries: months,
	}
}

func newAllowanceBreakdown(allowances []Allowance) *AllowanceBreakdown {
	return &AllowanceBreakdown{
		Allowances: allowances,
	}
}

func newCommissionBreakdown(commissions []Commission) *CommissionBreakdown {
	return &CommissionBreakdown{
		Commissions: commissions,
	}
}

func (c *Calculation) populate(product product, period, exchangeRate decimal.Decimal, incomes statMap) {
	c.Source = newSourceIncome(incomes, product, period)
	c.AllowanceBreakdown = incomes.toListAllowances()
	c.CommissionBreakdown = incomes.toListCommissions(period)
	c.SalaryBreakdown = incomes.toListMonthlySalaries()
	c.PeriodInMonth = period
	c.TotalBasicSalary = incomes.totalBasicSalary(product, period)
	c.TotalIncome = incomes.totalIncome(product)
	c.TotalOtherIncome = incomes.totalOtherIncome(period)
	c.MonthlyAverageIncome = incomes.averageMonthlyIncome(product, period)
	c.MonthlyNetIncome = incomes.netIncomeMonthly(product, exchangeRate, period)
	c.ExchangeRate = exchangeRate
}

type Account struct {
	Number      string `json:"number"`
	DisplayName string `json:"displayName"`
	Currency    string `json:"currency"`
}

type ListCalculationsResult struct {
	Calculations  []*Calculation `json:"calculations"`
	NextPageToken string         `json:"nextPageToken"`
}

// Source represents the source of income.
type Source struct {
	BasicSalary Breakdown `json:"basicSalary"`
	Allowance   Breakdown `json:"allowance"`
	Commission  Breakdown `json:"commission"`
	Other       Breakdown `json:"other"`
}

func (s *Source) Bytes() []byte {
	b, _ := json.Marshal(s)
	return b
}

type Breakdown struct {
	MonthlyAverage decimal.Decimal `json:"monthlyAverage"`
	Total          decimal.Decimal `json:"total"`
}

type MonthlySalary struct {
	Month         string          `json:"month"`
	TimesReceived decimal.Decimal `json:"timesReceived"`
	Transactions  []Transaction   `json:"transactions"`
	Total         decimal.Decimal `json:"total"`
}

type SalaryBreakdown struct {
	MonthlySalaries []MonthlySalary `json:"monthlySalaries"`
	BasicSalary     decimal.Decimal `json:"basicSalary"`
	Total           decimal.Decimal `json:"total"`
}

func (l *SalaryBreakdown) Bytes() []byte {
	if l.MonthlySalaries == nil {
		l.MonthlySalaries = []MonthlySalary{}
	}

	b, _ := json.Marshal(l)
	return b
}

type Allowance struct {
	Title          string          `json:"title"`
	Months         decimal.Decimal `json:"months"`
	MonthlyAverage decimal.Decimal `json:"monthlyAverage"`
	Transactions   []Transaction   `json:"transactions"`
	Total          decimal.Decimal `json:"total"`
}

type AllowanceBreakdown struct {
	Allowances []Allowance     `json:"allowances"`
	Total      decimal.Decimal `json:"total"`
}

func (l *AllowanceBreakdown) Bytes() []byte {
	if l.Allowances == nil {
		l.Allowances = []Allowance{}
	}

	b, _ := json.Marshal(l)
	return b
}

type Commission struct {
	Month        string          `json:"month"`
	Transactions []Transaction   `json:"transactions"`
	Total        decimal.Decimal `json:"total"`
}

type CommissionBreakdown struct {
	Commissions    []Commission    `json:"commissions"`
	MonthlyAverage decimal.Decimal `json:"monthlyAverage"`
	Total          decimal.Decimal `json:"total"`
}

func (l *CommissionBreakdown) Bytes() []byte {
	if l.Commissions == nil {
		l.Commissions = []Commission{}
	}

	b, _ := json.Marshal(l)
	return b
}

func newCalculation(by string, number, statementFileName string, product product) *Calculation {
	now := time.Now()
	return &Calculation{
		Number:               number,
		StatementFileName:    statementFileName,
		Product:              product,
		ExchangeRate:         decimal.NewFromInt(1),
		MonthlyAverageIncome: decimal.Zero,
		MonthlyNetIncome:     decimal.Zero,
		TotalOtherIncome:     decimal.Zero,
		TotalBasicSalary:     decimal.Zero,
		TotalIncome:          decimal.Zero,
		Status:               StatusPending,
		CreatedBy:            by,
		CreatedAt:            now,
		UpdatedBy:            by,
		UpdatedAt:            now,
	}
}

func (s *Calculation) toStateMap() (statMap, error) {
	m := make(statMap, 0)

	awnTxs := make(map[string][]Transaction, 0)
	awsMonthly := make([]decimal.Decimal, 0)
	awsAverageMonths := make(map[string]decimal.Decimal, 0)
	for _, a := range s.AllowanceBreakdown.Allowances {
		if len(a.Transactions) == 0 {
			continue
		}
		awnTxs[a.Title] = append(awnTxs[a.Title], a.Transactions...)
		awsMonthly = append(awsMonthly, a.Total)
		awsAverageMonths[a.Title] = a.Months
	}

	m[SourceAllowance.String()] = &statCal{
		Transactions: awnTxs,
		Total:        sumAmounts(awsMonthly),
		Monthly:      awsMonthly,
		AverageMonth: awsAverageMonths,
	}

	comTxs := make(map[string][]Transaction, 0)
	commMonthly := make([]decimal.Decimal, 0)
	for _, c := range s.CommissionBreakdown.Commissions {
		if len(c.Transactions) == 0 {
			continue
		}
		comTxs[c.Month] = append(comTxs[c.Month], c.Transactions...)
		commMonthly = append(commMonthly, c.Total)
	}

	m[SourceCommission.String()] = &statCal{
		Transactions: comTxs,
		Monthly:      commMonthly,
		Total:        sumAmounts(commMonthly),
	}

	salTxs := make(map[string][]Transaction, 0)
	salaryMonthly := make([]decimal.Decimal, 0)
	for _, s := range s.SalaryBreakdown.MonthlySalaries {
		if len(s.Transactions) == 0 {
			continue
		}
		salTxs[s.Month] = append(salTxs[s.Month], s.Transactions...)
		for _, t := range s.Transactions {
			salaryMonthly = append(salaryMonthly, t.Amount)
		}
	}

	m[SourceSalary.String()] = &statCal{
		Transactions: salTxs,
		Monthly:      salaryMonthly,
		Total:        sumAmounts(salaryMonthly),
	}

	return m, nil
}

type CalculationQuery struct {
	ID                 int64     `query:"id"`
	Product            string    `query:"product"`
	Number             string    `query:"number"`
	AccountDisplayName string    `query:"accountDisplayName"`
	CreatedAfter       time.Time `query:"createdAfter"`
	CreatedBefore      time.Time `query:"createdBefore"`
	PageSize           uint64    `query:"pageSize"`
	PageToken          string    `query:"pageToken"`
}

func (q *CalculationQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != 0 {
		and = append(and, sq.Eq{"id": q.ID})
	}
	if q.Product != "" {
		and = append(and, sq.Eq{"product": q.Product})
	}
	if q.Number != "" {
		and = append(and, sq.Expr("number LIKE ?", "%"+q.Number+"%"))
	}
	if q.AccountDisplayName != "" {
		and = append(and, sq.Expr("account_display_name LIKE ?", "%"+q.AccountDisplayName+"%"))
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

// saveCalculationIncome saves the calculation to the database.
func saveCalculationIncome(ctx context.Context, db *sql.DB, in *Calculation) error {
	return database.WithTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		updatedQuery, args := sq.Update("statement_file_analysis").
			Set("statement_file_name", in.StatementFileName).
			Set("number", in.Number).
			Set("product", in.Product).
			Set("account_currency", in.Account.Currency).
			Set("account_number", in.Account.Number).
			Set("account_display_name", in.Account.DisplayName).
			Set("exchange_rate", in.ExchangeRate).
			Set("total_income", in.TotalIncome).
			Set("total_basic_salary", in.TotalBasicSalary).
			Set("total_other_income", in.TotalOtherIncome).
			Set("monthly_net_income", in.MonthlyNetIncome).
			Set("monthly_average_income", in.MonthlyAverageIncome).
			Set("period_in_month", in.PeriodInMonth).
			Set("started_at", in.StartedAt).
			Set("ended_at", in.EndedAt).
			Set("status", in.Status.String()).
			Set("source_income", in.Source.Bytes()).
			Set("monthly_salary", in.SalaryBreakdown.Bytes()).
			Set("allowance", in.AllowanceBreakdown.Bytes()).
			Set("commission", in.CommissionBreakdown.Bytes()).
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

		rowsAffected, err := effected.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			insertQuery, args := sq.Insert("statement_file_analysis").
				Columns(
					"statement_file_name",
					"number",
					"product",
					"account_currency",
					"account_number",
					"account_display_name",
					"exchange_rate",
					"total_income",
					"total_basic_salary",
					"total_other_income",
					"monthly_net_income",
					"monthly_average_income",
					"period_in_month",
					"started_at",
					"ended_at",
					"status",
					"source_income",
					"monthly_salary",
					"allowance",
					"commission",
					"created_by",
					"created_at",
				).
				Values(
					in.StatementFileName,
					in.Number,
					in.Product,
					in.Account.Currency,
					in.Account.Number,
					in.Account.DisplayName,
					in.ExchangeRate,
					in.TotalIncome,
					in.TotalBasicSalary,
					in.TotalOtherIncome,
					in.MonthlyNetIncome,
					in.MonthlyAverageIncome,
					in.PeriodInMonth,
					in.StartedAt,
					in.EndedAt,
					in.Status.String(),
					in.Source.Bytes(),
					in.SalaryBreakdown.Bytes(),
					in.AllowanceBreakdown.Bytes(),
					in.CommissionBreakdown.Bytes(),
					in.CreatedBy,
					in.CreatedAt,
				).
				Suffix("SELECT SCOPE_IDENTITY()").
				PlaceholderFormat(sq.AtP).
				MustSql()

			row := tx.QueryRowContext(ctx, insertQuery, args...)
			if err := row.Scan(&in.ID); err != nil {
				return fmt.Errorf("failed to insert calculation: %w", err)
			}
			return nil
		}

		return nil
	})
}

func listCalculations(ctx context.Context, db *sql.DB, in *CalculationQuery) ([]*Calculation, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))

	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.Select(
		id,
		"statement_file_name",
		"number",
		"product",
		"account_currency",
		"account_number",
		"account_display_name",
		"exchange_rate",
		"total_income",
		"total_basic_salary",
		"total_other_income",
		"monthly_net_income",
		"monthly_average_income",
		"period_in_month",
		"started_at",
		"ended_at",
		"status",
		"source_income",
		"monthly_salary",
		"allowance",
		"commission",
		"created_by",
		"created_at",
		"updated_by",
		"updated_at",
	).
		From("statement_file_analysis").
		Where(pred, args...).
		OrderBy("created_at DESC").
		PlaceholderFormat(sq.AtP).
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]*Calculation, 0)
	for rows.Next() {
		c := new(Calculation)
		var source, salaries, allowances, commissions []byte
		err := rows.Scan(
			&c.ID,
			&c.StatementFileName,
			&c.Number,
			&c.Product,
			&c.Account.Currency,
			&c.Account.Number,
			&c.Account.DisplayName,
			&c.ExchangeRate,
			&c.TotalIncome,
			&c.TotalBasicSalary,
			&c.TotalOtherIncome,
			&c.MonthlyNetIncome,
			&c.MonthlyAverageIncome,
			&c.PeriodInMonth,
			&c.StartedAt,
			&c.EndedAt,
			&c.Status,
			&source,
			&salaries,
			&allowances,
			&commissions,
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

		component := new(Source)
		if err := json.Unmarshal(source, component); err != nil {
			return nil, fmt.Errorf("failed to unmarshal source: %w", err)
		}

		salaryBreakdown := new(SalaryBreakdown)
		if err := json.Unmarshal(salaries, salaryBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal salary breakdown: %w", err)
		}

		allowanceBreakdown := new(AllowanceBreakdown)
		if err := json.Unmarshal(allowances, allowanceBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal allowance breakdown: %w", err)
		}

		commissionBreakdown := new(CommissionBreakdown)
		if err := json.Unmarshal(commissions, commissionBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal commission breakdown: %w", err)
		}

		c.Source = component
		c.SalaryBreakdown = salaryBreakdown
		c.AllowanceBreakdown = allowanceBreakdown
		c.CommissionBreakdown = commissionBreakdown

		calculations = append(calculations, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over calculations: %w", err)
	}

	return calculations, nil
}

func isCalculationExists(ctx context.Context, db *sql.DB, number string) (bool, error) {
	q, args := sq.Select("TOP 1 number").
		From("statement_file_analysis").
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

type Transaction struct {
	Date       ddmmyyyy        `json:"date"`
	BillNumber string          `json:"billNumber"`
	Noted      string          `json:"noted"`
	Amount     decimal.Decimal `json:"amount"`
}

type ListTransactionsResult struct {
	Transactions []*Transaction `json:"transactions"`
}

type TransactionReq struct {
	// The statement calculation number
	Number string `json:"number" param:"number"`

	// Category is the source of the income
	Category source `json:"category"`

	// Month in MMYYYY format
	Month mmyyyy `json:"month"`
}

func (r *TransactionReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.Month.Time().IsZero() {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "month",
			Description: "Month must not be empty",
		})
	}

	if r.Category == SourceUnSpecified {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "category",
			Description: "Category must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Transaction is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type GetTransactionReq struct {
	Number     string `json:"number" param:"number"`
	BillNumber string `json:"billNumber" param:"billNumber"`
}

func (r *GetTransactionReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.BillNumber == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "billNumber",
			Description: "Bill number must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Transaction is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type BatchGetCalculationsQuery struct {
	ID                 int64     `query:"id"`
	Product            string    `query:"product"`
	Number             string    `query:"number"`
	AccountDisplayName string    `query:"accountDisplayName"`
	CreatedAfter       time.Time `query:"createdAfter"`
	CreatedBefore      time.Time `query:"createdBefore"`

	nextID int64
}

func (q *BatchGetCalculationsQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != 0 {
		and = append(and, sq.Eq{"id": q.ID})
	}
	if q.Product != "" {
		and = append(and, sq.Eq{"product": q.Product})
	}
	if q.Number != "" {
		and = append(and, sq.Expr("number LIKE ?", "%"+q.Number+"%"))
	}
	if q.AccountDisplayName != "" {
		and = append(and, sq.Expr("account_display_name LIKE ?", "%"+q.AccountDisplayName+"%"))
	}

	if !q.CreatedAfter.IsZero() {
		and = append(and, sq.GtOrEq{"created_at": q.CreatedAfter})
	}

	if !q.CreatedBefore.IsZero() {
		and = append(and, sq.LtOrEq{"created_at": q.CreatedBefore})
	}

	if q.nextID > 0 {
		and = append(and, sq.Lt{"id": q.nextID})
	}

	return and.ToSql()
}

func batchGetCalculations(ctx context.Context, db *sql.DB, batchSize int, nextID int64, in *BatchGetCalculationsQuery) ([]*Calculation, error) {
	id := fmt.Sprintf("TOP %d id", batchSize)
	in.nextID = nextID
	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.Select(
		id,
		"statement_file_name",
		"number",
		"product",
		"account_currency",
		"account_number",
		"account_display_name",
		"exchange_rate",
		"total_income",
		"total_basic_salary",
		"total_other_income",
		"monthly_net_income",
		"monthly_average_income",
		"period_in_month",
		"started_at",
		"ended_at",
		"status",
		"source_income",
		"monthly_salary",
		"allowance",
		"commission",
		"created_by",
		"created_at",
		"updated_by",
		"updated_at",
	).
		From("statement_file_analysis").
		Where(pred, args...).
		OrderBy("ID DESC").
		PlaceholderFormat(sq.AtP).
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]*Calculation, 0)
	for rows.Next() {
		c := new(Calculation)
		var source, salaries, allowances, commissions []byte
		err := rows.Scan(
			&c.ID,
			&c.StatementFileName,
			&c.Number,
			&c.Product,
			&c.Account.Currency,
			&c.Account.Number,
			&c.Account.DisplayName,
			&c.ExchangeRate,
			&c.TotalIncome,
			&c.TotalBasicSalary,
			&c.TotalOtherIncome,
			&c.MonthlyNetIncome,
			&c.MonthlyAverageIncome,
			&c.PeriodInMonth,
			&c.StartedAt,
			&c.EndedAt,
			&c.Status,
			&source,
			&salaries,
			&allowances,
			&commissions,
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

		component := new(Source)
		if err := json.Unmarshal(source, component); err != nil {
			return nil, fmt.Errorf("failed to unmarshal source: %w", err)
		}

		salaryBreakdown := new(SalaryBreakdown)
		if err := json.Unmarshal(salaries, salaryBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal salary breakdown: %w", err)
		}

		allowanceBreakdown := new(AllowanceBreakdown)
		if err := json.Unmarshal(allowances, allowanceBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal allowance breakdown: %w", err)
		}

		commissionBreakdown := new(CommissionBreakdown)
		if err := json.Unmarshal(commissions, commissionBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal commission breakdown: %w", err)
		}

		c.Source = component
		c.SalaryBreakdown = salaryBreakdown
		c.AllowanceBreakdown = allowanceBreakdown
		c.CommissionBreakdown = commissionBreakdown

		calculations = append(calculations, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over calculations: %w", err)
	}

	return calculations, nil
}
