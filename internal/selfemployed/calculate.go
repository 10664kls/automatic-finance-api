package selfemployed

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/database"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	"github.com/10664kls/automatic-finance-api/internal/statement"
	"github.com/10664kls/automatic-finance-api/internal/types"
	sq "github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
	edpb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcstatus "google.golang.org/grpc/status"
)

// ErrCalculationNotFound is returned when a calculation is not found in the database.
var ErrCalculationNotFound = errors.New("calculation not found")

func getCurrencyCodeFromStatementFile(file *statement.StatementFile) (string, error) {
	f, err := excelize.OpenFile(file.Location)
	if err != nil {
		return "", fmt.Errorf("failed to open statement file: %w", err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rawAccountCurrency, err := f.GetCellValue(sheetName, "A11")
	if err != nil {
		return "", fmt.Errorf("failed to get account currency from cell A11: %w", err)
	}

	currencyCode := extractAccount(rawAccountCurrency)
	if len(strings.TrimSpace(currencyCode)) != 3 {
		return "", fmt.Errorf("no valid income transactions found in the statement file %s", file.Location)
	}

	return currencyCode, nil
}

func calculateIncomeFromStatementFile(
	ctx context.Context,
	in *CalculateReq,
) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)
	calculation := newCalculation(claims.Username, in)

	f, err := excelize.OpenFile(in.file.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open statement file: %w", err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rawPeriod, err := f.GetCellValue(sheetName, "A7")
	if err != nil {
		return nil, fmt.Errorf("failed to get period from cell A7: %w", err)
	}

	from, to := extractPeriod(rawPeriod)
	calculation.StartedAt = from
	calculation.EndedAt = to

	rawAccountNumber, err := f.GetCellValue(sheetName, "A9")
	if err != nil {
		return nil, fmt.Errorf("failed to get account number from cell A9: %w", err)
	}

	rawAccountDisplayName, err := f.GetCellValue(sheetName, "A10")
	if err != nil {
		return nil, fmt.Errorf("failed to get account display name from cell A10: %w", err)
	}

	rawAccountCurrency, err := f.GetCellValue(sheetName, "A11")
	if err != nil {
		return nil, fmt.Errorf("failed to get account currency from cell A11: %w", err)
	}

	calculation.Account.Number = extractAccount(rawAccountNumber)
	calculation.Account.DisplayName = extractAccount(rawAccountDisplayName)
	calculation.Account.Currency = extractAccount(rawAccountCurrency)

	if len(calculation.Account.Number) == 0 || len(calculation.Account.DisplayName) == 0 || len(strings.TrimSpace(calculation.Account.Currency)) != 3 {
		return nil, fmt.Errorf("no valid income transactions found in the statement file %s", in.file.Location)
	}

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows from sheet %s: %w", sheetName, err)
	}
	defer rows.Close()

	period := countMonth(calculation.StartedAt, calculation.EndedAt)
	state := new(stateCal)
	state.ExchangeRate = in.currency.ExchangeRate
	state.MarginPercentage = in.business.MarginPercentage
	state.PeriodInMonth = period

	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get columns from row: %w", err)
		}

		if len(row) <= 4 {
			continue // skip rows with insufficient columns
		}

		rawAmount := strings.ReplaceAll(row[4], ",", "")
		incomeAmount, err := decimal.NewFromString(rawAmount)
		if err != nil || incomeAmount.LessThanOrEqual(decimal.Zero) {
			continue // skip if the amount is not valid
		}

		if len(row[2]) == 0 {
			continue // skip if the word is empty
		}

		if matched := matchWordlist(row[2], in.wordlists); !matched {
			continue // skip if the word does not match any wordlist
		}

		date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
		if err != nil {
			continue // skip if the date is not valid
		}

		transaction := Transaction{
			Amount:     incomeAmount,
			Date:       types.DDMMYYYY(date),
			BillNumber: row[1],
			Noted:      row[2],
		}

		month := getMonthWithYYYYMM(row[0])
		if state.Transactions == nil {
			state.Transactions = make(map[string][]Transaction, 0)
		}

		if _, ok := state.Transactions[month]; !ok {
			state.Transactions[month] = []Transaction{}
		}

		state.Total = state.Total.Add(incomeAmount)
		state.Transactions[month] = append(state.Transactions[month], transaction)
	}

	calculation.populate(state)
	return calculation, nil
}

type stateCal struct {
	Transactions     map[string][]Transaction
	Total            decimal.Decimal
	ExchangeRate     decimal.Decimal
	MarginPercentage decimal.Decimal
	PeriodInMonth    decimal.Decimal
}

func (s *stateCal) averageMonthlyIncome() decimal.Decimal {
	if s.PeriodInMonth.IsZero() {
		return decimal.Zero
	}

	return s.Total.Div(s.PeriodInMonth)
}

func (s *stateCal) averageMonthlyIncomeByMargin() decimal.Decimal {
	if s.PeriodInMonth.IsZero() || s.MarginPercentage.IsZero() {
		return decimal.Zero
	}

	hundred := decimal.NewFromInt(100)
	percent := s.MarginPercentage.Div(hundred)
	averageMonthly := s.averageMonthlyIncome()

	return averageMonthly.Mul(percent)
}

func (s *stateCal) getMonthlyNetIncome() decimal.Decimal {
	if s.PeriodInMonth.IsZero() || s.MarginPercentage.IsZero() {
		return decimal.Zero
	}

	average := s.averageMonthlyIncomeByMargin()
	return average.Mul(s.ExchangeRate)
}

func (s *stateCal) toMonthlyBreakdown() *MonthlyBreakdown {
	monthlyIncomes := make([]MonthlyIncome, 0)
	for month, ts := range s.Transactions {
		if len(ts) == 0 {
			continue // skip if the month has no transactions
		}

		tx := MonthlyIncome{
			Month:         month,
			TimesReceived: decimal.NewFromInt(int64(len(ts))),
			Transactions:  ts,
			Total:         sumTransactions(ts),
		}

		monthlyIncomes = append(monthlyIncomes, tx)
	}

	sort.Slice(monthlyIncomes, func(i, j int) bool {
		ti, _ := time.Parse("January-2006", monthlyIncomes[i].Month)
		tj, _ := time.Parse("January-2006", monthlyIncomes[j].Month)
		return ti.Before(tj)
	})

	return &MonthlyBreakdown{
		MonthlyIncomes: monthlyIncomes,
		Total:          s.Total,
	}
}

type Calculation struct {
	ID                     int64                `json:"id"`
	StatementFileName      string               `json:"statementFileName"`
	Number                 string               `json:"number"`
	BusinessType           BusinessType         `json:"businessType"`
	Product                types.ProductType    `json:"product"`
	Account                Account              `json:"account"`
	StartedAt              time.Time            `json:"startedAt"`
	EndedAt                time.Time            `json:"endedAt"`
	PeriodInMonth          decimal.Decimal      `json:"periodInMonth"`
	ExchangeRate           decimal.Decimal      `json:"exchangeRate"`
	MarginPercentage       decimal.Decimal      `json:"marginPercentage"`
	TotalIncome            decimal.Decimal      `json:"totalIncome"`
	MonthlyAverageIncome   decimal.Decimal      `json:"monthlyAverageIncome"`
	MonthlyAverageByMargin decimal.Decimal      `json:"monthlyAverageByMargin"`
	MonthlyNetIncome       decimal.Decimal      `json:"monthlyNetIncome"` // Monthly net income after margin in LAK.
	MonthlyBreakdown       *MonthlyBreakdown    `json:"monthlyBreakdown"`
	Status                 types.AnalysisStatus `json:"status"`
	CreatedBy              string               `json:"createdBy"`
	UpdatedBy              string               `json:"updatedBy"`
	CreatedAt              time.Time            `json:"createdAt"`
	UpdatedAt              time.Time            `json:"updatedAt"`
}

func (c *Calculation) Complete(by string) {
	c.Status = types.StatusCompleted
	c.UpdatedAt = time.Now()
	c.UpdatedBy = by
}

// IsCompleted returns true if the calculation has completed analysis,
// and false otherwise.
func (c *Calculation) IsCompleted() bool {
	return c.Status == types.StatusCompleted
}

// Recalculate updates the calculation with new income data.
// It recalculates the monthly breakdown, average income, net income,
// and updates the internal state of the calculation based on the
// provided RecalculateReq. The updatedBy and updatedAt fields are
// also set to reflect the user making the update and the current time.

func (c *Calculation) Recalculate(by string, in *RecalculateReq) {
	c.MonthlyBreakdown = in.toMonthlyBreakdown()
	state := c.toStateCal()
	c.UpdatedAt = time.Now()
	c.UpdatedBy = by
	c.populate(state)
}

func (c *Calculation) populate(state *stateCal) {
	c.MonthlyBreakdown = state.toMonthlyBreakdown()
	c.PeriodInMonth = state.PeriodInMonth
	c.MonthlyAverageIncome = state.averageMonthlyIncome()
	c.MonthlyAverageByMargin = state.averageMonthlyIncomeByMargin()
	c.MonthlyNetIncome = state.getMonthlyNetIncome()
	c.ExchangeRate = state.ExchangeRate
	c.MarginPercentage = state.MarginPercentage
	c.TotalIncome = state.Total
}

func (c *Calculation) toStateCal() *stateCal {
	txMap := make(map[string][]Transaction, 0)
	var total decimal.Decimal
	for _, m := range c.MonthlyBreakdown.MonthlyIncomes {
		if len(m.Transactions) == 0 {
			continue // skip if the month has no transactions
		}

		txMap[m.Month] = append(txMap[m.Month], m.Transactions...)
		total = total.Add(m.Total)
	}

	return &stateCal{
		ExchangeRate:     c.ExchangeRate,
		MarginPercentage: c.MarginPercentage,
		PeriodInMonth:    c.PeriodInMonth,
		Transactions:     txMap,
		Total:            total,
	}
}

type Account struct {
	DisplayName string `json:"displayName"`
	Number      string `json:"number"`
	Currency    string `json:"currency"`
}

type BusinessType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MonthlyBreakdown struct {
	MonthlyIncomes []MonthlyIncome `json:"monthlyIncomes"`
	Total          decimal.Decimal `json:"total"`
}

func (s MonthlyBreakdown) Length() decimal.Decimal {
	if s.MonthlyIncomes == nil {
		return decimal.Zero
	}

	return decimal.NewFromInt(int64(len(s.MonthlyIncomes)))
}

func (l *MonthlyBreakdown) Bytes() []byte {
	if l.MonthlyIncomes == nil {
		l.MonthlyIncomes = []MonthlyIncome{}
	}

	b, _ := json.Marshal(l)
	return b
}

type MonthlyIncome struct {
	Month         string          `json:"month"`
	TimesReceived decimal.Decimal `json:"timesReceived"`
	Transactions  []Transaction   `json:"transactions"`
	Total         decimal.Decimal `json:"total"`
}

type Transaction struct {
	Date       types.DDMMYYYY  `json:"date"`
	BillNumber string          `json:"billNumber"`
	Noted      string          `json:"noted"`
	Amount     decimal.Decimal `json:"amount"`
}

type TransactionQuery struct {
	// The statement calculation number
	Number string `json:"number" param:"number"`

	// Month in MMYYYY format
	Month types.MMYYY `json:"month"`

	// These must be set before listing transactions.
	wordlists []*Wordlist
	file      *statement.StatementFile
}

// Populate sets the fields of the request that are not part of the request but must be set before listing transactions.
// It is used for setting the fields from the database before the calculation.
func (r *TransactionQuery) Populate(file *statement.StatementFile, wordlists []*Wordlist) {
	r.file = file
	r.wordlists = wordlists
}

func (r *TransactionQuery) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.Month.Time().IsZero() {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "month",
			Description: "Month must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Transaction is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

func listIncomeTransactionsFromStatementFile(req *TransactionQuery) ([]*Transaction, error) {
	if req.file == nil {
		return nil, errors.New("statement file must be set before listing transactions")
	}
	if req.wordlists == nil {
		return nil, errors.New("wordlists must be set before listing transactions")
	}

	f, err := excelize.OpenFile(req.file.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", req.file.Name, err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows: %w", err)
	}
	defer rows.Close()

	ts := make([]*Transaction, 0)
	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get row columns: %w", err)
		}

		if len(row) <= 4 {
			continue // skip if the row has less than 4 columns
		}

		rawAmount := strings.ReplaceAll(row[4], ",", "")
		incomeAmount, err := decimal.NewFromString(rawAmount)
		if err != nil {
			continue // skip if the amount is invalid
		}

		if incomeAmount.LessThanOrEqual(decimal.Zero) || len(row[2]) == 0 {
			continue // skip if the amount is zero or the description is empty
		}

		if matched := matchWordlist(row[2], req.wordlists); !matched {
			continue // skip if the description does not match any wordlist
		}

		date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
		if err != nil {
			continue // skip if the date is invalid
		}

		if strings.Compare(date.Format("January-2006"), req.Month.String()) != 0 {
			continue // skip if the date is not the same month as the monthly income
		}

		ts = append(ts, &Transaction{
			Amount:     incomeAmount,
			Date:       types.DDMMYYYY(date),
			BillNumber: row[1],
			Noted:      row[2],
		})
	}

	return ts, nil
}

func getIncomeTransactionByBillNumber(req *GetTransactionQuery) (*Transaction, error) {
	if req.file == nil {
		return nil, errors.New("Statement file must be set before getting a transaction")
	}

	f, err := excelize.OpenFile(req.file.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", req.file.Name, err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get row: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get row columns: %w", err)
		}

		if len(row) <= 4 {
			continue // skip if the row has less than 4 columns
		}

		rawAmount := strings.ReplaceAll(row[4], ",", "")
		incomeAmount, err := decimal.NewFromString(rawAmount)
		if err != nil {
			continue // skip if the amount is invalid
		}

		if incomeAmount.LessThanOrEqual(decimal.Zero) || len(row[2]) == 0 {
			continue // skip if the amount is zero or the description is empty
		}

		if strings.TrimSpace(strings.ToLower(row[1])) != strings.TrimSpace(strings.ToLower(req.BillNumber)) {
			continue
		}

		date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
		if err != nil {
			continue // skip if the date is invalid
		}

		return &Transaction{
			Date:       types.DDMMYYYY(date),
			Noted:      row[2],
			BillNumber: row[1],
			Amount:     incomeAmount,
		}, nil
	}

	return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this transaction or (it may not exist)")
}

type GetTransactionQuery struct {
	Number     string `json:"number" param:"number"`
	BillNumber string `json:"billNumber" param:"billNumber"`

	// These must be set before getting the transaction.
	file *statement.StatementFile
}

// Populate sets the file field of the request that is not part of the request but must be set before getting the transaction.
// It is used for setting the fields from the database before the calculation.
func (r *GetTransactionQuery) Populate(file *statement.StatementFile) {
	r.file = file
}

func (r *GetTransactionQuery) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.BillNumber == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "billNumber",
			Description: "Bill number must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Transaction is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}
func newCalculation(by string, in *CalculateReq) *Calculation {
	now := time.Now()

	return &Calculation{
		CreatedBy: by,
		UpdatedBy: by,
		CreatedAt: now,
		UpdatedAt: now,
		BusinessType: BusinessType{
			ID:   in.business.ID,
			Name: in.business.Name,
		},
		MarginPercentage:  in.business.MarginPercentage,
		Product:           in.Product,
		Number:            in.Number,
		StatementFileName: in.StatementFileName,
		Status:            types.StatusPending,
	}
}

func countMonth(from, to time.Time) decimal.Decimal {
	if to.Before(from) {
		return decimal.Zero
	}

	yearDiff := to.Year() - from.Year()
	monthDiff := int(to.Month()) - int(from.Month())

	return decimal.NewFromInt(int64(yearDiff*12 + monthDiff))
}

func extractAccount(raw string) string {
	raw = strings.TrimSpace(raw)
	r := strings.Split(raw, " : ")
	if len(r) != 2 {
		return ""
	}

	return strings.TrimSpace(r[1])
}

func extractPeriod(raw string) (from, to time.Time) {
	raw = strings.TrimSpace(raw)
	r := strings.Split(raw, " : ")
	if len(r) != 2 {
		return
	}

	period := strings.Split(r[1], " ຫາ ")

	from, err := time.ParseInLocation("02/01/2006", period[0], time.Local)
	if err != nil {
		return
	}

	to, err = time.ParseInLocation("02/01/2006", period[1], time.Local)
	if err != nil {
		return
	}

	return
}

func sumTransactions(ts []Transaction) decimal.Decimal {
	if len(ts) == 0 {
		return decimal.Zero
	}

	sum := decimal.Zero
	for _, t := range ts {
		sum = sum.Add(t.Amount)
	}
	return sum
}

func getMonthWithYYYYMM(s string) string {
	m, err := time.Parse("02/01/2006", s)
	if err != nil {
		return ""
	}
	return m.Format("January-2006")
}

type CalculateReq struct {
	Product           types.ProductType `json:"product"`
	Number            string            `json:"number"`
	BusinessID        string            `json:"businessId"`
	StatementFileName string            `json:"statementFileName"`

	// These fields are used for the calculation.
	// They are not part of the request but must be set before the calculation.
	file      *statement.StatementFile
	business  *Business
	currency  *currency.Currency
	wordlists []*Wordlist
}

// Populate sets the fields of the request that are not part of the request but must be set before the calculation.
// It is used for setting the fields from the database before the calculation.
func (r *CalculateReq) Populate(file *statement.StatementFile, business *Business, currency *currency.Currency, wordlists []*Wordlist) {
	r.file = file
	r.business = business
	r.currency = currency
	r.wordlists = wordlists
}

func (r *CalculateReq) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.Product == types.ProductUnSpecified {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "product",
			Description: "Product must be a valid product.",
		})
	}

	if r.StatementFileName == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "statementFileName",
			Description: "Statement file name must not be empty",
		})
	}

	if r.BusinessID == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "businessId",
			Description: "Business ID must not be empty",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type RecalculateReq struct {
	Number         string             `param:"number"`
	MonthlyIncomes []MonthlyIncomeReq `json:"monthlyIncomes"`
}

func (r *RecalculateReq) toMonthlyBreakdown() *MonthlyBreakdown {
	ms := make([]MonthlyIncome, len(r.MonthlyIncomes))
	var total decimal.Decimal
	for i, mi := range r.MonthlyIncomes {
		ms[i] = MonthlyIncome{
			Month:         mi.Month,
			Transactions:  mi.Transactions,
			Total:         sumTransactions(mi.Transactions),
			TimesReceived: decimal.NewFromInt(int64(len(mi.Transactions))),
		}

		total = total.Add(ms[i].Total)
	}

	return &MonthlyBreakdown{
		MonthlyIncomes: ms,
		Total:          total,
	}
}

func (r *RecalculateReq) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if len(r.MonthlyIncomes) == 0 {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "monthlyIncomes",
			Description: "Monthly incomes must not be empty",
		})
	}

	for i, mi := range r.MonthlyIncomes {
		if err := validateMonthlyIncome(&mi); err != nil {
			violations = append(violations, &edpb.BadRequest_FieldViolation{
				Field:       "monthlyIncomes",
				Description: fmt.Sprintf("Monthly income at index %d is not valid: %s", i, err),
			})
		}
	}

	if len(violations) > 0 {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type MonthlyIncomeReq struct {
	Month        string        `json:"month"`
	Transactions []Transaction `json:"transactions"`
}

func validateMonthlyIncome(r *MonthlyIncomeReq) error {
	if r.Month == "" {
		return fmt.Errorf("month must not be empty")
	}
	if _, err := types.ParseMMYYYY("January-2006", r.Month); err != nil {
		return fmt.Errorf("invalid month: %w", err)
	}

	if len(r.Transactions) == 0 {
		return fmt.Errorf("transactions must not be empty")
	}

	for i, t := range r.Transactions {
		if t.Date.Time().Format("January-2006") != r.Month {
			return fmt.Errorf("transaction at index %d must have the same month as the monthly income", i)
		}

		if err := validationTransaction(&t); err != nil {
			return fmt.Errorf("transaction at index %d is not valid: %w", i, err)
		}
	}

	return nil
}

func validationTransaction(t *Transaction) error {
	if t.Amount.IsZero() {
		return errors.New("amount must not be empty")
	}

	if t.Amount.LessThan(decimal.Zero) {
		return errors.New("amount must not be negative")
	}

	if t.Date.Time().IsZero() {
		return errors.New("date must not be empty")
	}

	if t.BillNumber == "" {
		return errors.New("bill number must not be empty")
	}

	if t.Noted == "" {
		return errors.New("noted must not be empty")
	}

	return nil
}

func isCalculationExists(ctx context.Context, db *sql.DB, number string) (bool, error) {
	q, args := sq.Select("TOP 1 number").
		From("self_employed_analysis").
		Where(
			sq.Eq{
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

func saveCalculationIncome(ctx context.Context, db *sql.DB, in *Calculation) error {
	return database.WithTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		updatedQuery, args := sq.Update("self_employed_analysis").
			Set("statement_file_name", in.StatementFileName).
			Set("business_type_id", in.BusinessType.ID).
			Set("product", in.Product).
			Set("account_currency", in.Account.Currency).
			Set("account_number", in.Account.Number).
			Set("account_display_name", in.Account.DisplayName).
			Set("period_in_month", in.PeriodInMonth).
			Set("started_at", in.StartedAt).
			Set("ended_at", in.EndedAt).
			Set("exchange_rate", in.ExchangeRate).
			Set("margin_percentage", in.MarginPercentage).
			Set("total_income", in.TotalIncome).
			Set("monthly_average_income", in.MonthlyAverageIncome).
			Set("monthly_average_margin", in.MonthlyAverageByMargin).
			Set("monthly_net_income", in.MonthlyNetIncome).
			Set("source_income", in.MonthlyBreakdown.Bytes()).
			Set("status", in.Status.String()).
			Set("updated_by", in.UpdatedBy).
			Set("updated_at", in.UpdatedAt).
			Where(
				sq.Eq{
					"number": in.Number,
				},
			).
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
			insertQuery, args := sq.Insert("self_employed_analysis").
				Columns(
					"number",
					"statement_file_name",
					"business_type_id",
					"product",
					"account_currency",
					"account_number",
					"account_display_name",
					"period_in_month",
					"started_at",
					"ended_at",
					"exchange_rate",
					"margin_percentage",
					"total_income",
					"monthly_average_income",
					"monthly_average_margin",
					"monthly_net_income",
					"source_income",
					"status",
					"created_by",
					"created_at",
					"updated_by",
					"updated_at",
				).
				Values(
					in.Number,
					in.StatementFileName,
					in.BusinessType.ID,
					in.Product,
					in.Account.Currency,
					in.Account.Number,
					in.Account.DisplayName,
					in.PeriodInMonth,
					in.StartedAt,
					in.EndedAt,
					in.ExchangeRate,
					in.MarginPercentage,
					in.TotalIncome,
					in.MonthlyAverageIncome,
					in.MonthlyAverageByMargin,
					in.MonthlyNetIncome,
					in.MonthlyBreakdown.Bytes(),
					in.Status.String(),
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
		}

		return nil
	})
}

type CalculationQuery struct {
	ID                 int64     `query:"id"`
	Product            string    `query:"product"`
	Number             string    `query:"number"`
	BusinessTypeID     string    `query:"businessTypeID"`
	AccountDisplayName string    `query:"accountDisplayName"`
	CreatedAfter       time.Time `query:"createdAfter"`
	CreatedBefore      time.Time `query:"createdBefore"`
	PageSize           uint64    `query:"pageSize"`
	PageToken          string    `query:"pageToken"`
}

func (q *CalculationQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != 0 {
		and = append(and, sq.Eq{"s.id": q.ID})
	}
	if q.Product != "" {
		and = append(and, sq.Eq{"product": q.Product})
	}
	if q.Number != "" {
		and = append(and, sq.Eq{"number": q.Number})
	}
	if q.AccountDisplayName != "" {
		and = append(and, sq.Expr("account_display_name LIKE ?", "%"+q.AccountDisplayName+"%"))
	}
	if q.BusinessTypeID != "" {
		and = append(and, sq.Eq{"business_type_id": q.BusinessTypeID})
	}

	if !q.CreatedAfter.IsZero() {
		and = append(and, sq.GtOrEq{"s.created_at": q.CreatedAfter})
	}

	if !q.CreatedBefore.IsZero() {
		and = append(and, sq.LtOrEq{"s.created_at": q.CreatedBefore})
	}

	if q.PageToken != "" {
		cursor, err := pager.DecodeCursor(q.PageToken)
		if err == nil {
			and = append(and, sq.Lt{"s.created_at": cursor.Time})
		}
	}

	return and.ToSql()
}

func listCalculations(ctx context.Context, db *sql.DB, in *CalculationQuery) ([]*Calculation, error) {
	id := fmt.Sprintf("TOP %d s.id", pager.Size(in.PageSize))

	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.Select(
		id,
		"number",
		"statement_file_name",
		"b.id",
		"b.name",
		"product",
		"account_currency",
		"account_number",
		"account_display_name",
		"period_in_month",
		"started_at",
		"ended_at",
		"exchange_rate",
		"s.margin_percentage",
		"total_income",
		"monthly_average_income",
		"monthly_average_margin",
		"monthly_net_income",
		"source_income",
		"status",
		"s.created_by",
		"s.created_at",
		"s.updated_by",
		"s.updated_at",
	).
		From("self_employed_analysis AS s").
		LeftJoin("business_type AS b ON s.business_type_id = b.id").
		Where(pred, args...).
		OrderBy("s.created_at DESC").
		PlaceholderFormat(sq.AtP).
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]*Calculation, 0)
	for rows.Next() {
		var byt []byte
		c := new(Calculation)
		err := rows.Scan(
			&c.ID,
			&c.Number,
			&c.StatementFileName,
			&c.BusinessType.ID,
			&c.BusinessType.Name,
			&c.Product,
			&c.Account.Currency,
			&c.Account.Number,
			&c.Account.DisplayName,
			&c.PeriodInMonth,
			&c.StartedAt,
			&c.EndedAt,
			&c.ExchangeRate,
			&c.MarginPercentage,
			&c.TotalIncome,
			&c.MonthlyAverageIncome,
			&c.MonthlyAverageByMargin,
			&c.MonthlyNetIncome,
			&byt,
			&c.Status,
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

		monthlyBreakdown := new(MonthlyBreakdown)
		if err := json.Unmarshal(byt, monthlyBreakdown); err != nil {
			return nil, fmt.Errorf("failed to unmarshal monthly breakdown: %w", err)
		}

		c.MonthlyBreakdown = monthlyBreakdown
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
