package selfemployed

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	"github.com/10664kls/automatic-finance-api/internal/statement"
	"go.uber.org/zap"
	edpb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcstatus "google.golang.org/grpc/status"
)

type Service struct {
	db        *sql.DB
	statement *statement.Service
	currency  *currency.Service
	mu        *sync.Mutex
	zlog      *zap.Logger
}

func NewService(_ context.Context, db *sql.DB, statement *statement.Service, currency *currency.Service, zlog *zap.Logger) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}
	if statement == nil {
		return nil, errors.New("statement service is nil")
	}
	if currency == nil {
		return nil, errors.New("currency service is nil")
	}

	return &Service{
		db:        db,
		statement: statement,
		currency:  currency,
		zlog:      zlog,
		mu:        new(sync.Mutex),
	}, nil
}

type ListBusinessesResult struct {
	Businesses    []*Business `json:"businesses"`
	NextPageToken string      `json:"nextPageToken"`
}

func (s *Service) ListBusinesses(ctx context.Context, in *BusinessQuery) (*ListBusinessesResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("method", "ListBusinesses"),
		zap.Any("req", in),
		zap.String("username", claims.Username),
	)

	businesses, err := listBusinesses(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to list businesses", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(businesses); l > 0 && l == int(pager.Size(in.PageSize)) {
		last := businesses[l-1]
		pageToken = pager.EncodeCursor(&pager.Cursor{
			ID:   last.ID,
			Time: last.CreatedAt,
		})
	}

	return &ListBusinessesResult{
		Businesses:    businesses,
		NextPageToken: pageToken,
	}, nil
}

func (s *Service) GetBusinessByID(ctx context.Context, id string) (*Business, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("method", "GetBusinessByID"),
		zap.String("id", id),
		zap.String("username", claims.Username),
	)

	business, err := getBusiness(ctx, s.db, &BusinessQuery{ID: id})
	if errors.Is(err, ErrBusinessNotFound) {
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to access this business or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get business by ID", zap.Error(err))
		return nil, err
	}

	return business, nil
}

func (s *Service) CreateBusiness(ctx context.Context, in *BusinessReq) (*Business, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("method", "CreateBusiness"),
		zap.Any("req", in),
		zap.String("username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	exists, err := isBusinessExists(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to check if business exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcstatus.Error(codes.AlreadyExists, "The business with this name already exists")
	}

	business := newBusiness(claims.Username, in.Name, in.Description, in.MarginPercentage)
	if err := createBusiness(ctx, s.db, business); err != nil {
		zlog.Error("failed to create business", zap.Error(err))
		return nil, err
	}

	return business, nil
}

func (s *Service) UpdateBusiness(ctx context.Context, in *BusinessReq) (*Business, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("method", "UpdateBusiness"),
		zap.Any("req", in),
		zap.String("username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	business, err := getBusiness(ctx, s.db, &BusinessQuery{ID: in.ID})
	if errors.Is(err, ErrBusinessNotFound) {
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to access this business or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get business by ID", zap.Error(err))
		return nil, err
	}

	exists, err := isBusinessExists(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to check if business exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcstatus.Error(codes.AlreadyExists, "The business with this name already exists")
	}

	business.update(claims.Username, in.Name, in.Description, in.MarginPercentage)
	if err := updateBusiness(ctx, s.db, business); err != nil {
		zlog.Error("failed to update business", zap.Error(err))
		return nil, err
	}

	return business, nil
}

func (s *Service) CalculateIncome(ctx context.Context, req *CalculateReq) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("method", "CalculateIncome"),
		zap.Any("req", req),
		zap.String("username", claims.Username),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	exists, err := isCalculationExists(ctx, s.db, req.Number)
	if err != nil {
		zlog.Error("failed to check if calculation exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcstatus.Error(codes.AlreadyExists, "Calculation with this number already exists. Please use a different number.")
	}

	file, err := s.statement.GetStatementByName(ctx, req.StatementFileName)
	if st, ok := rpcstatus.FromError(err); ok && st.Code() == codes.PermissionDenied {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: []*edpb.BadRequest_FieldViolation{
				{
					Field:       "statementFileName",
					Description: "Statement file must be a valid file name",
				},
			},
		})

		return nil, s.Err()
	}
	if err != nil {
		return nil, err
	}

	business, err := s.GetBusinessByID(ctx, req.BusinessID)
	if st, ok := rpcstatus.FromError(err); ok && st.Code() == codes.PermissionDenied {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: []*edpb.BadRequest_FieldViolation{
				{
					Field:       "businessId",
					Description: "Business ID must be a valid business ID",
				},
			},
		})

		return nil, s.Err()
	}
	if err != nil {
		return nil, err
	}

	wordlists, err := listWordlists(ctx, s.db, &WordlistQuery{noLimit: true})
	if err != nil {
		zlog.Error("failed to list wordlists", zap.Error(err))
		return nil, err
	}

	currencyCode, err := getCurrencyCodeFromStatementFile(file)
	if err != nil {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: []*edpb.BadRequest_FieldViolation{
				{
					Field:       "statementFileName",
					Description: "Statement file must be a valid file name",
				},
			},
		})

		zlog.Error("failed to get currency code from statement file", zap.Error(err))
		return nil, s.Err()
	}

	currency, err := s.currency.GetCurrencyByCode(ctx, currencyCode)
	if st, ok := rpcstatus.FromError(err); ok && st.Code() == codes.PermissionDenied {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: []*edpb.BadRequest_FieldViolation{
				{
					Field:       "statementFileName",
					Description: "Statement file must be a valid file name",
				},
			},
		})

		return nil, s.Err()
	}
	if err != nil {
		return nil, err
	}

	req.Populate(file, business, currency, wordlists)
	calculation, err := calculateIncomeFromStatementFile(ctx, req)
	if err != nil {
		zlog.Error("failed to calculate income from statement file", zap.Error(err))
		return nil, err
	}

	if err := saveCalculationIncome(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to save calculation", zap.Error(err))
		return nil, err
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
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

type ListCalculationsResult struct {
	Calculations  []*Calculation `json:"calculations"`
	NextPageToken string         `json:"nextPageToken"`
}

func (s *Service) ListCalculations(ctx context.Context, in *CalculationQuery) (*ListCalculationsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListCalculations"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
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
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
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

func (s *Service) ReCalculateIncome(ctx context.Context, req *RecalculateReq) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ReCalculateIncome"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: req.Number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	if calculation.IsCompleted() {
		return nil, rpcstatus.Error(codes.FailedPrecondition, "This calculation is already completed and cannot be recalculated")
	}

	calculation.Recalculate(claims.Username, req)
	if err := saveCalculationIncome(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to save calculation", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

type ListTransactionsResult struct {
	Transactions []*Transaction `json:"transactions"`
}

func (s *Service) ListIncomeTransactionsByNumber(ctx context.Context, req *TransactionQuery) (*ListTransactionsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListIncomeTransactionsByNumber"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	calculation, err := s.GetCalculationByNumber(ctx, req.Number)
	if err != nil {
		return nil, err
	}

	file, err := s.statement.GetStatementByName(ctx, calculation.StatementFileName)
	if err != nil {
		return nil, err
	}

	wordlists, err := listWordlists(ctx, s.db, &WordlistQuery{noLimit: true})
	if err != nil {
		zlog.Error("failed to get wordlists", zap.Error(err))
		return nil, err
	}

	req.Populate(file, wordlists)
	transactions, err := listIncomeTransactionsFromStatementFile(req)
	if err != nil {
		zlog.Error("failed to list transactions", zap.Error(err))
		return nil, err
	}

	return &ListTransactionsResult{
		Transactions: transactions,
	}, nil
}

func (s *Service) GetIncomeTransactionByBillNumber(ctx context.Context, req *GetTransactionQuery) (*Transaction, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetIncomeTransactionByBillNumber"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	calculation, err := s.GetCalculationByNumber(ctx, req.Number)
	if err != nil {
		return nil, err
	}

	file, err := s.statement.GetStatementByName(ctx, calculation.StatementFileName)
	if err != nil {
		return nil, err
	}

	req.Populate(file)
	transaction, err := getIncomeTransactionByBillNumber(req)
	if err != nil {
		zlog.Error("failed to get transaction", zap.Error(err))
		return nil, err
	}

	return transaction, nil
}

type ListWordlistsResult struct {
	Wordlists     []*Wordlist `json:"wordlists"`
	NextPageToken string      `json:"nextPageToken"`
}

func (s *Service) ListWordlists(ctx context.Context, req *WordlistQuery) (*ListWordlistsResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListWordlists"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	wordlists, err := listWordlists(ctx, s.db, req)
	if err != nil {
		zlog.Error("failed to list wordlists", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(wordlists); l > 0 && l == int(pager.Size(req.PageSize)) {
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
		zap.Int64("id", id),
	)

	wordlist, err := getWordlist(ctx, s.db, &WordlistQuery{ID: id})
	if errors.Is(err, ErrWordlistNotFound) {
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this wordlist or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get wordlist", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) CreateWordlist(ctx context.Context, req *WordlistReq) (*Wordlist, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CreateWordlist"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	exists, err := isWordlistExists(ctx, s.db, req)
	if err != nil {
		zlog.Error("failed to check if wordlist exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcstatus.Error(codes.AlreadyExists, "The word already exists. Please try again with a different word.")
	}

	wordlist := req.ToWordlist(claims.Username)
	if err := saveWordlist(ctx, s.db, wordlist); err != nil {
		zlog.Error("failed to save wordlist", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) UpdateWordlist(ctx context.Context, req *WordlistReq) (*Wordlist, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "UpdateWordlist"),
		zap.String("Username", claims.Username),
		zap.Any("req", req),
	)

	if err := req.Validate(); err != nil {
		return nil, err
	}

	wordlist, err := s.GetWordlistByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}

	exists, err := isWordlistExists(ctx, s.db, req)
	if err != nil {
		zlog.Error("failed to check if wordlist exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcstatus.Error(codes.AlreadyExists, "The word already exists. Please try again with a different word.")
	}

	wordlist.update(claims.Username, req.Word)
	if err := saveWordlist(ctx, s.db, wordlist); err != nil {
		zlog.Error("failed to save wordlist", zap.Error(err))
		return nil, err
	}

	return wordlist, nil
}

func (s *Service) ExportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	claims := auth.ClaimsFromContext(ctx)
	zlog := s.zlog.With(
		zap.String("Service", "selfemployed"),
		zap.String("Method", "ExportCalculationsToExcel"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
	)

	byt, err := s.exportCalculationsToExcel(ctx, in)
	if err != nil {
		zlog.Error("failed to export calculations to excel", zap.Error(err))
		return nil, err
	}

	return byt, nil
}

func (s *Service) ExportCalculationToExcelByNumber(ctx context.Context, number string) (*bytes.Buffer, error) {
	claims := auth.ClaimsFromContext(ctx)
	zlog := s.zlog.With(
		zap.String("Service", "selfemployed"),
		zap.String("Method", "ExportCalculationToExcelByNumber"),
		zap.String("Username", claims.Username),
		zap.String("Number", number),
	)

	calculation, err := getCalculation(ctx, s.db, &CalculationQuery{
		Number: number,
	})
	if errors.Is(err, ErrCalculationNotFound) {
		return nil, rpcstatus.Error(codes.PermissionDenied, "You are not allowed to this calculation or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get calculation by number", zap.Error(err))
		return nil, err
	}

	buf, err := exportCalculationToExcel(calculation)
	if err != nil {
		zlog.Error("failed to export calculation to excel", zap.Error(err))
		return nil, err
	}

	return buf, nil
}
