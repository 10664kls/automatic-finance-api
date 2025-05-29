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
	CreatedBy            string          `json:"createdBy"`
	CreatedAt            time.Time       `json:"createdAt"`

	SalaryBreakdown     *SalaryBreakdown     `json:"salaryBreakdown"`
	AllowanceBreakdown  *AllowanceBreakdown  `json:"allowanceBreakdown"`
	CommissionBreakdown *CommissionBreakdown `json:"commissionBreakdown"`
	Source              *Source              `json:"source"`
}

func (c *Calculation) ReCalculate(in *RecalculateReq) error {
	c.SalaryBreakdown = newSalaryBreakdown(in.MonthlySalaries)
	c.AllowanceBreakdown = newAllowanceBreakdown(in.Allowances)
	c.CommissionBreakdown = newCommissionBreakdown(in.Commissions)

	mapCal, err := c.toStateMap()
	if err != nil {
		return fmt.Errorf("failed to convert calculation to state map: %w", err)
	}

	c.populate(c.Product, c.PeriodInMonth, c.ExchangeRate, mapCal)
	return nil
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
	Month                 string            `json:"month"`
	TimesReceived         decimal.Decimal   `json:"timesReceived"`
	ActualAmountsReceived []decimal.Decimal `json:"actualAmountsReceived"`
	Total                 decimal.Decimal   `json:"total"`
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
	Month                 string            `json:"month"`
	ActualAmountsReceived []decimal.Decimal `json:"actualAmountsReceived"`
	Total                 decimal.Decimal   `json:"total"`
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
		CreatedBy:            by,
		CreatedAt:            time.Now(),
	}
}

func (s *Calculation) toStateMap() (statMap, error) {
	m := make(statMap, 0)

	awsActualAmountsReceived := make(map[string][]decimal.Decimal, 0)
	awsMonthly := make([]decimal.Decimal, 0)
	awsAverageMonths := make(map[string]decimal.Decimal, 0)
	for _, a := range s.AllowanceBreakdown.Allowances {
		awsActualAmountsReceived[a.Title] = append(awsActualAmountsReceived[a.Title], a.Total)
		awsMonthly = append(awsMonthly, a.Total)
		awsAverageMonths[a.Title] = a.Months
	}

	m[SourceAllowance.String()] = &statCal{
		ActualAmountsReceived: awsActualAmountsReceived,
		Total:                 sumAmounts(awsMonthly),
		Monthly:               awsMonthly,
		AverageMonth:          awsAverageMonths,
	}

	commActualAmountsReceived := make(map[string][]decimal.Decimal, 0)
	commMonthly := make([]decimal.Decimal, 0)
	for _, c := range s.CommissionBreakdown.Commissions {
		commActualAmountsReceived[c.Month] = append(commActualAmountsReceived[c.Month], c.ActualAmountsReceived...)
		commMonthly = append(commMonthly, c.Total)
	}

	m[SourceCommission.String()] = &statCal{
		ActualAmountsReceived: commActualAmountsReceived,
		Monthly:               commMonthly,
		Total:                 sumAmounts(commMonthly),
	}

	salActualAmountsReceived := make(map[string][]decimal.Decimal, 0)
	salaryMonthly := make([]decimal.Decimal, 0)
	for _, s := range s.SalaryBreakdown.MonthlySalaries {
		salActualAmountsReceived[s.Month] = append(salActualAmountsReceived[s.Month], s.ActualAmountsReceived...)
		salaryMonthly = append(salaryMonthly, s.ActualAmountsReceived...)
	}

	m[SourceSalary.String()] = &statCal{
		ActualAmountsReceived: salActualAmountsReceived,
		Monthly:               salaryMonthly,
		Total:                 sumAmounts(salaryMonthly),
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
			Set("source_income", in.Source.Bytes()).
			Set("monthly_salary", in.SalaryBreakdown.Bytes()).
			Set("allowance", in.AllowanceBreakdown.Bytes()).
			Set("commission", in.CommissionBreakdown.Bytes()).
			Set("created_by", in.CreatedBy).
			Set("created_at", in.CreatedAt).
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
		"source_income",
		"monthly_salary",
		"allowance",
		"commission",
		"created_by",
		"created_at",
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
			&source,
			&salaries,
			&allowances,
			&commissions,
			&c.CreatedBy,
			&c.CreatedAt,
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
