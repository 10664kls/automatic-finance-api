package income

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
	"go.uber.org/zap"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

type Service struct {
	currency *currency.Service
	db       *sql.DB
	mu       *sync.Mutex
	zlog     *zap.Logger
}

func NewService(_ context.Context, db *sql.DB, currency *currency.Service, zlog *zap.Logger) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}
	if currency == nil {
		return nil, errors.New("currency service is nil")
	}

	return &Service{
		db:       db,
		currency: currency,
		zlog:     zlog,
		mu:       new(sync.Mutex),
	}, nil
}

func (s *Service) ListWordlists(ctx context.Context, in *WordlistQuery) (*ListWordlistsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListWordlists"),
		zap.String("Username", claims.Username),
	)

	wordlists, err := listWordlists(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to list wordlists", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(wordlists); l > 0 && l == int(pager.Size(in.PageSize)) {
		last := wordlists[l-1]
		pageToken = pager.EncodeCursor(&pager.Cursor{
			ID:   strconv.FormatInt(last.ID, 10),
			Time: last.CreatedAt,
		})
	}

	return &ListWordlistsResult{
		Wordlists:     wordlists,
		NextPageToken: pageToken,
	}, nil
}

func (s *Service) GetWordlistByID(ctx context.Context, id int64) (*Wordlist, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetWordlistByID"),
		zap.String("Username", claims.Username),
		zap.Int64("ID", id),
	)

	wordlist, err := getWordlist(ctx, s.db, &WordlistQuery{
		ID: id,
	})
	if errors.Is(err, ErrWordlistNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get wordlist by ID", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) CreateWordlist(ctx context.Context, in *WordlistReq) (*Wordlist, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CreateWordlist"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	wordlist := in.ToWordlist(claims.Username)
	if err := saveWordlist(ctx, s.db, wordlist); err != nil {
		zlog.Error("failed to save wordlist", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) UpdateWordlist(ctx context.Context, in *WordlistReq) (*Wordlist, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "UpdateWordlist"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	wordlist, err := getWordlist(ctx, s.db, &WordlistQuery{
		ID: in.ID,
	})
	if errors.Is(err, ErrWordlistNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get wordlist by ID", zap.Error(err))
		return nil, err
	}

	wordlist.Update(claims.Username, in)
	if err := saveWordlist(ctx, s.db, wordlist); err != nil {
		zlog.Error("failed to save wordlist", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) CalculateIncome(ctx context.Context, in *CalculateReq) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CalculateIncome"),
		zap.String("Username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	exists, err := isCalculationExists(ctx, s.db, in.Number)
	if err != nil {
		return nil, fmt.Errorf("failed to check if calculation exists: %w", err)
	}
	if exists {
		return nil, rpcStatus.New(
			codes.AlreadyExists,
			"Calculation with this number already exists. Please use a different number.",
		).Err()
	}

	statementFile, err := getStatementFileByName(ctx, s.db, in.StatementFileName)
	if errors.Is(err, ErrStatementFileNotFound) {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: []*edPb.BadRequest_FieldViolation{
				{
					Field:       "statementFileName",
					Description: "Statement file must be a valid file name",
				},
			},
		})

		return nil, s.Err()
	}
	if err != nil {
		zlog.Error("failed to get statement file", zap.Error(err))
		return nil, err
	}

	wordlists, err := listWordlists(ctx, s.db, &WordlistQuery{
		noLimit: true,
	})
	if err != nil {
		zlog.Error("failed to get wordlists", zap.Error(err))
		return nil, err
	}

	calculation, err := s.calculateIncomeFromStatementFile(ctx, in, wordlists, statementFile)
	if err != nil {
		zlog.Warn("failed to calculate income from statement file", zap.Error(err))
		return nil, rpcStatus.
			Error(
				codes.FailedPrecondition,
				"The statement file is not valid. Please check your statement file and try again.",
			)
	}

	if err := saveCalculationIncome(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to save calculation income", zap.Error(err))
		return nil, fmt.Errorf("failed to save calculation income: %w", err)
	}

	return calculation, nil
}

func (s *Service) GetCalculationByNumber(ctx context.Context, number string) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetCalculationByNumber"),
		zap.String("Username", claims.Username),
		zap.String("Number", number),
	)

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

func (s *Service) ListCalculations(ctx context.Context, in *CalculationQuery) (*ListCalculationsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListCalculations"),
		zap.String("Username", claims.Username),
	)

	calculations, err := listCalculations(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to list calculations", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(calculations); l > 0 && l == int(pager.Size(in.PageSize)) {
		last := calculations[l-1]
		pageToken = pager.EncodeCursor(&pager.Cursor{
			ID:   strconv.FormatInt(last.ID, 10),
			Time: last.CreatedAt,
		})
	}

	return &ListCalculationsResult{
		Calculations:  calculations,
		NextPageToken: pageToken,
	}, nil
}

type RecalculateReq struct {
	Number                   string          `param:"number"`
	BasicSalaryFromInterview decimal.Decimal `json:"basicSalaryFromInterview"`
	MonthlySalaries          []MonthlySalary `json:"monthlySalaries"`
	Allowances               []Allowance     `json:"allowances"`
	Commissions              []Commission    `json:"commissions"`
}

func (s *Service) ReCalculateIncome(ctx context.Context, in *RecalculateReq) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ReCalculateIncome"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: in.Number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}
	if calculation.IsCompleted() {
		return nil, rpcStatus.Error(codes.FailedPrecondition, "This calculation is already completed and cannot be recalculated")
	}

	if err := calculation.ReCalculate(claims.Username, in); err != nil {
		zlog.Error("failed to recalculate income", zap.Error(err))
		return nil, err
	}

	if err := saveCalculationIncome(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to save calculation", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

func (s *Service) CompleteCalculation(ctx context.Context, number string) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CompleteCalculation"),
		zap.String("Username", claims.Username),
		zap.Any("number", number),
	)

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	if calculation.IsCompleted() {
		return calculation, nil
	}

	calculation.Complete(claims.Username)
	if err := saveCalculationIncome(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to save calculation", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

func (s *Service) ListIncomeTransactionsByNumber(ctx context.Context, in *TransactionReq) (*ListTransactionsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListIncomeTransactionsByNumber"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: in.Number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	statementFile, err := getStatementFileByName(ctx, s.db, calculation.StatementFileName)
	if errors.Is(err, ErrStatementFileNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this statement file or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get statement file", zap.Error(err))
		return nil, err
	}

	wordlists, err := listWordlists(ctx, s.db, &WordlistQuery{
		Category: in.Category.String(),
		noLimit:  true,
	})
	if err != nil {
		zlog.Error("failed to get wordlists", zap.Error(err))
		return nil, err
	}

	txs, err := s.listTransactionFromStatementFile(ctx, in, wordlists, statementFile)
	if err != nil {
		zlog.Error("failed to list transactions", zap.Error(err))
		return nil, err
	}

	return &ListTransactionsResult{Transactions: txs}, nil
}

func (s *Service) GetIncomeTransactionByBillNumber(ctx context.Context, in *GetTransactionReq) (*Transaction, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetIncomeTransactionByBillNumber"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: in.Number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	statementFile, err := getStatementFileByName(ctx, s.db, calculation.StatementFileName)
	if errors.Is(err, ErrStatementFileNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this statement file or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get statement file", zap.Error(err))
		return nil, err
	}

	f, err := excelize.OpenFile(statementFile.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", statementFile.Name, err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get row columns: %w", err)
		}

		if len(row) > 4 {
			incomeAmount, err := decimal.NewFromString(strings.ReplaceAll(row[4], ",", ""))
			if err != nil {
				continue
			}
			if incomeAmount.GreaterThan(decimal.Zero) && len(row[2]) > 0 {
				if strings.TrimSpace(strings.ToLower(row[1])) == strings.TrimSpace(strings.ToLower(in.BillNumber)) {
					date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
					if err != nil {
						continue
					}

					return &Transaction{
						BillNumber: row[1],
						Noted:      row[2],
						Date:       ddmmyyyy(date),
						Amount:     incomeAmount,
					}, nil
				}
			}
		}
	}

	return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
}

func (s *Service) ExportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	return s.exportCalculationsToExcel(ctx, in)
}

func (s *Service) ExportCalculationToExcelByNumber(ctx context.Context, number string) (*bytes.Buffer, error) {
	claims := auth.ClaimsFromContext(ctx)
	zlog := s.zlog.With(
		zap.String("Method", "ExportCalculationToExcelByNumber"),
		zap.String("Username", claims.Username),
		zap.String("Number", number),
	)

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	buf, err := exportCalculationToExcel(ctx, calculation)
	if err != nil {
		zlog.Error("failed to export calculation to excel", zap.Error(err))
		return nil, err
	}

	return buf, nil
}

func (s *Service) listTransactionFromStatementFile(ctx context.Context, txReq *TransactionReq, wordlists []*Wordlist, statement *StatementFile) ([]*Transaction, error) {
	f, err := excelize.OpenFile(statement.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", statement.Name, err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows: %w", err)
	}
	defer rows.Close()

	txs := make([]*Transaction, 0)
	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get row columns: %w", err)
		}

		if len(row) > 4 {
			incomeAmount, err := decimal.NewFromString(strings.ReplaceAll(row[4], ",", ""))
			if err != nil {
				continue
			}
			if incomeAmount.GreaterThan(decimal.Zero) && len(row[2]) > 0 {
				if _, _, exist := matchWordlists(row[2], wordlists); exist {
					date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
					if err != nil {
						continue
					}

					if strings.Compare(date.Format("January-2006"), txReq.Month.String()) == 0 {
						txs = append(txs, &Transaction{
							Amount:     incomeAmount,
							Date:       ddmmyyyy(date),
							BillNumber: row[1],
							Noted:      row[2],
						})
					}
				}
			}
		}
	}

	return txs, nil
}

func (s *Service) calculateIncomeFromStatementFile(ctx context.Context, cal *CalculateReq, wordlists []*Wordlist, statement *StatementFile) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)
	calculation := newCalculation(claims.Username, cal.Number, statement.Name, cal.Product)

	f, err := excelize.OpenFile(statement.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", statement.Name, err)
	}
	defer f.Close()

	const sheetName = "Table 1"

	rawPeriod, err := f.GetCellValue(sheetName, "A7")
	if err != nil {
		return nil, fmt.Errorf("failed to get period: %w", err)
	}

	from, to := extractPeriod(rawPeriod)
	calculation.StartedAt = from
	calculation.EndedAt = to

	rawAccountNumber, err := f.GetCellValue(sheetName, "A9")
	if err != nil {
		return nil, fmt.Errorf("failed to get account number: %w", err)
	}

	rawAccountDisplayName, err := f.GetCellValue(sheetName, "A10")
	if err != nil {
		return nil, fmt.Errorf("failed to get account display name: %w", err)
	}

	rawAccountCurrency, err := f.GetCellValue(sheetName, "A11")
	if err != nil {
		return nil, fmt.Errorf("failed to get account currency: %w", err)
	}

	calculation.Account.Number = extractAccount(rawAccountNumber)
	calculation.Account.DisplayName = extractAccount(rawAccountDisplayName)
	calculation.Account.Currency = extractAccount(rawAccountCurrency)

	if len(calculation.Account.Number) == 0 || len(calculation.Account.DisplayName) == 0 || len(strings.TrimSpace(calculation.Account.Currency)) != 3 {
		return nil, fmt.Errorf("no valid income transactions found in the statement file %s", statement.Name)
	}

	currency, err := s.currency.GetCurrencyByCode(ctx, calculation.Account.Currency)
	if err != nil {
		return nil, err
	}

	rows, err := f.Rows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to get rows from sheet %s: %w", sheetName, err)
	}
	defer rows.Close()

	incomes := make(statMap, 0)
	keyAw := SourceAllowance.String()
	keySy := SourceSalary.String()
	keyCom := SourceCommission.String()
	defaultMonths := decimal.NewFromInt(12)
	for rows.Next() {
		row, err := rows.Columns()
		if err != nil {
			return nil, fmt.Errorf("failed to get row columns: %w", err)
		}

		if len(row) <= 4 {
			continue // skip rows with insufficient columns
		}

		// Parse amount
		rawAmount := strings.ReplaceAll(row[4], ",", "")
		incomeAmount, err := decimal.NewFromString(rawAmount)
		if err != nil || !incomeAmount.GreaterThan(decimal.Zero) {
			continue // skip invalid or zero amounts
		}

		if len(row[2]) == 0 {
			continue // skip if the note field is empty
		}

		// Parse date
		date, err := time.ParseInLocation("02/01/2006", row[0], time.Local)
		if err != nil {
			continue // skip if date is invalid
		}
		month := getMonthWithYYYYMM(row[0])

		// Match note field with wordlist
		category, title, matched := matchWordlists(row[2], wordlists)
		if !matched {
			continue
		}

		transaction := Transaction{
			Amount:     incomeAmount,
			Date:       ddmmyyyy(date),
			BillNumber: row[1],
			Noted:      row[2],
		}

		switch category {
		case SourceSalary:
			if _, ok := incomes[keySy]; !ok {
				incomes[keySy] = &statCal{
					Transactions: make(map[string][]Transaction),
				}
			}
			incomes[keySy].Monthly = append(incomes[keySy].Monthly, incomeAmount)
			incomes[keySy].Total = incomes[keySy].Total.Add(incomeAmount)
			incomes[keySy].Transactions[month] = append(incomes[keySy].Transactions[month], transaction)

		case SourceCommission:
			if _, ok := incomes[keyCom]; !ok {
				incomes[keyCom] = &statCal{
					Transactions: make(map[string][]Transaction),
				}
			}
			incomes[keyCom].Monthly = append(incomes[keyCom].Monthly, incomeAmount)
			incomes[keyCom].Total = incomes[keyCom].Total.Add(incomeAmount)
			incomes[keyCom].Transactions[month] = append(incomes[keyCom].Transactions[month], transaction)

		case SourceAllowance:
			if _, ok := incomes[keyAw]; !ok {
				incomes[keyAw] = &statCal{
					Transactions: make(map[string][]Transaction),
					AverageMonth: make(map[string]decimal.Decimal),
				}
			}
			incomes[keyAw].Monthly = append(incomes[keyAw].Monthly, incomeAmount)
			incomes[keyAw].Total = incomes[keyAw].Total.Add(incomeAmount)
			incomes[keyAw].AverageMonth[title] = defaultMonths
			incomes[keyAw].Transactions[title] = append(incomes[keyAw].Transactions[title], transaction)
		}
	}

	period := countMonth(from, to)
	calculation.populate(cal.Product, period, currency.ExchangeRate, incomes)
	return calculation, nil
}

func newSourceIncome(m statMap, product product, period decimal.Decimal) *Source {
	return &Source{
		Allowance: Breakdown{
			Total:          m.toListAllowances().Total,
			MonthlyAverage: m.averageAllowance(period),
		},
		Commission: Breakdown{
			Total:          m.toListCommissions(period).Total,
			MonthlyAverage: m.averageCommission(period),
		},
		BasicSalary: Breakdown{
			Total:          m.totalBasicSalary(product, period),
			MonthlyAverage: m.basicSalary(product, period),
		},
	}
}

type statCal struct {
	Transactions map[string][]Transaction
	Monthly      []decimal.Decimal
	Total        decimal.Decimal

	// This used for allowance calculate the average.
	AverageMonth map[string]decimal.Decimal
}

type statMap map[string]*statCal

func (s statMap) totalIncome(product product) decimal.Decimal {
	switch product {
	case ProductSA:
		raw, ok := s[SourceSalary.String()]
		if !ok {
			return decimal.Zero
		}

		total := decimal.Zero
		for _, tx := range raw.Transactions {
			total = total.Add(findMinFromTransactions(tx))
		}
		return total

	case ProductPL, ProductSF:
		raw, ok := s[SourceSalary.String()]
		if !ok {
			return decimal.Zero
		}

		return raw.Total
	}

	return decimal.Zero
}

func (s statMap) basicSalaryFromInterview() decimal.Decimal {
	raw, ok := s[SourceBasicSalaryInterview.String()]
	if !ok {
		return decimal.Zero
	}

	return raw.Total
}

func (s statMap) totalBasicSalary(product product, period decimal.Decimal) decimal.Decimal {
	if period.IsZero() {
		return decimal.Zero
	}

	switch product {
	case ProductSA:
		total := s.totalIncome(ProductSA)
		if total.IsZero() {
			return decimal.Zero
		}

		return total.Div(period)

	case ProductPL, ProductSF:
		return s.basicSalary(product, period).Mul(period)
	}

	return decimal.Zero
}

func findMinAmountFromMonthlySalaries(ms []MonthlySalary) decimal.Decimal {
	if len(ms) == 0 {
		return decimal.Zero
	}

	min := ms[0].Total
	for _, m := range ms {
		if m.Total.LessThan(min) {
			min = m.Total
		}
	}

	return min
}

func (s statMap) totalOtherIncome(period decimal.Decimal) decimal.Decimal {
	if period.IsZero() {
		return decimal.Zero
	}

	o := s.totalIncome(ProductPL).Sub(s.totalBasicSalary(ProductPL, period))
	if o.LessThan(decimal.Zero) {
		return decimal.Zero
	}

	return o
}

func (s statMap) averageOtherIncome(period decimal.Decimal) decimal.Decimal {
	if period.IsZero() {
		return decimal.Zero
	}

	total := s.totalOtherIncome(period)
	if total.IsZero() {
		return decimal.Zero
	}

	return total.Div(period)
}

func (s statMap) averageCommission(period decimal.Decimal) decimal.Decimal {
	return s.toListCommissions(period).MonthlyAverage
}

func (s statMap) averageAllowance(period decimal.Decimal) decimal.Decimal {
	return s.toListAllowances().Total
}

func (s statMap) averageOtherIncomeIn80Percent(period decimal.Decimal) decimal.Decimal {
	// Assuming 80% of the total other income is considered
	other := s.averageOtherIncome(period)
	other = other.Add(s.averageCommission(period))
	other = other.Add(s.averageAllowance(period))
	return other.Mul(decimal.NewFromFloat(0.8))
}

func (s statMap) averageMonthlyIncome(product product, period decimal.Decimal) decimal.Decimal {
	switch product {
	case ProductSA:
		basic := s.basicSalary(ProductSA, period)
		interview := s.basicSalaryFromInterview()
		if interview.GreaterThan(decimal.Zero) && interview.LessThan(basic) {
			return interview.
				Add(s.averageAllowance(period)).
				Add(s.averageCommission(period))
		}

		return basic.
			Add(s.averageAllowance(period)).
			Add(s.averageCommission(period))

	case ProductPL, ProductSF:
		otherIn80Percent := s.averageOtherIncomeIn80Percent(period)
		basic := s.basicSalary(product, period)
		interview := s.basicSalaryFromInterview()
		if interview.GreaterThan(decimal.Zero) && interview.LessThan(basic) {
			return interview.Add(otherIn80Percent)
		}

		return basic.Add(otherIn80Percent)
	}

	return decimal.Zero
}

func (s statMap) netIncomeMonthly(product product, exchangeRate decimal.Decimal, period decimal.Decimal) decimal.Decimal {
	if period.IsZero() {
		return decimal.Zero
	}

	monthlyIncome := s.averageMonthlyIncome(product, period)
	if monthlyIncome.IsZero() {
		return decimal.Zero
	}

	return monthlyIncome.Mul(exchangeRate)
}

func (s statMap) basicSalary(product product, period decimal.Decimal) decimal.Decimal {
	switch product {
	case ProductSA:
		if period.IsZero() {
			return decimal.Zero
		}

		total := s.totalIncome(ProductSA)
		if total.IsZero() {
			return decimal.Zero
		}

		return total.Div(period)

	case ProductPL, ProductSF:
		return findMinAmountFromMonthlySalaries(s.toListMonthlySalaries().MonthlySalaries)
	}

	return decimal.Zero
}

func (s statMap) toListAllowances() *AllowanceBreakdown {
	raw, ok := s[SourceAllowance.String()]
	if !ok {
		return &AllowanceBreakdown{}
	}

	allowances := make([]Allowance, 0)
	totalAllowance := decimal.Zero
	months := decimal.NewFromInt(12)
	for title, tx := range raw.Transactions {
		if len(tx) == 0 {
			continue
		}

		if m, ok := raw.AverageMonth[title]; ok {
			months = m
		}

		amount := sumTransactions(tx)
		average := amount.Div(months)
		allowances = append(allowances, Allowance{
			Title:          title,
			Months:         months,
			MonthlyAverage: average,
			Total:          amount,
			Transactions:   tx,
		})

		totalAllowance = totalAllowance.Add(average)
	}

	return &AllowanceBreakdown{
		Allowances: allowances,
		Total:      totalAllowance,
	}
}

func (s statMap) toListCommissions(period decimal.Decimal) *CommissionBreakdown {

	raw, ok := s[SourceCommission.String()]
	if !ok {
		return &CommissionBreakdown{}
	}

	commissions := make([]Commission, 0)
	for month, tx := range raw.Transactions {
		if len(tx) == 0 {
			continue
		}
		commissions = append(commissions, Commission{
			Month:        month,
			Transactions: tx,
			Total:        sumTransactions(tx),
		})
	}

	sort.Slice(commissions, func(i, j int) bool {
		ti, _ := time.Parse("January-2006", commissions[i].Month)
		tj, _ := time.Parse("January-2006", commissions[j].Month)
		return ti.Before(tj)
	})

	monthlyAverage := decimal.Zero
	if !period.IsZero() {
		monthlyAverage = raw.Total.Div(period)
	}

	return &CommissionBreakdown{
		Commissions:    commissions,
		Total:          raw.Total,
		MonthlyAverage: monthlyAverage,
	}
}

func (s statMap) toListMonthlySalaries() *SalaryBreakdown {
	raw, ok := s[SourceSalary.String()]
	if !ok {
		return &SalaryBreakdown{}
	}

	monthlySalaries := make([]MonthlySalary, 0)
	for month, tx := range raw.Transactions {
		if len(tx) == 0 {
			continue
		}

		transaction := MonthlySalary{
			Month:         month,
			TimesReceived: decimal.NewFromInt(int64(len(tx))),
			Total:         sumTransactions(tx),
			Transactions:  tx,
		}
		monthlySalaries = append(monthlySalaries, transaction)
	}

	sort.Slice(monthlySalaries, func(i, j int) bool {
		ti, _ := time.Parse("January-2006", monthlySalaries[i].Month)
		tj, _ := time.Parse("January-2006", monthlySalaries[j].Month)
		return ti.Before(tj)
	})

	return &SalaryBreakdown{
		MonthlySalaries: monthlySalaries,
		BasicSalary:     findMinAmount(raw.Monthly),
		Total:           raw.Total,
	}
}

func findMinAmount(amounts []decimal.Decimal) decimal.Decimal {
	if len(amounts) == 0 {
		return decimal.Zero
	}

	min := amounts[0]
	for _, amount := range amounts {
		if amount.LessThan(min) {
			min = amount
		}
	}
	return min
}

func findMinFromTransactions(ts []Transaction) decimal.Decimal {
	if len(ts) == 0 {
		return decimal.Zero
	}

	min := ts[0].Amount
	for _, t := range ts {
		if t.Amount.LessThan(min) {
			min = t.Amount
		}
	}
	return min
}

func sumAmounts(amounts []decimal.Decimal) decimal.Decimal {
	if len(amounts) == 0 {
		return decimal.Zero
	}

	sum := decimal.Zero
	for _, amount := range amounts {
		sum = sum.Add(amount)
	}
	return sum
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

func getMonthWithYYYYMM(s string) string {
	m, err := time.Parse("02/01/2006", s)
	if err != nil {
		return ""
	}
	return m.Format("January-2006")
}

type CalculateReq struct {
	Number            string  `json:"number"`
	Product           product `json:"product"`
	StatementFileName string  `json:"statementFileName"`
}

func (r *CalculateReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Number == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "number",
			Description: "Number must not be empty",
		})
	}

	if r.Product == ProductUnSpecified {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "product",
			Description: "Product must be a valid product.",
		})
	}

	if r.StatementFileName == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "statementFileName",
			Description: "Statement file name must not be empty",
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
