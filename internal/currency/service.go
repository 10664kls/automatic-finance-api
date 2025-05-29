package currency

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/gen"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	"github.com/biter777/countries"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

// ErrCurrencyNotFound is returned when a currency is not found in the database.
var ErrCurrencyNotFound = errors.New("currency not found")

type Service struct {
	db   *sql.DB
	zlog *zap.Logger
}

func NewService(_ context.Context, db *sql.DB, zlog *zap.Logger) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}

	return &Service{
		db:   db,
		zlog: zlog,
	}, nil
}

func (s *Service) CreateCurrency(ctx context.Context, in *CreateReq) (*Currency, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CreateCurrency"),
		zap.String("Username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	currency := newCurrency(claims.Username, in)
	exists, err := isCurrencyCodeExists(ctx, s.db, currency.ID, currency.Code)
	if err != nil {
		zlog.Error("failed to check if currency exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcStatus.Error(codes.AlreadyExists, "The currency with this code already exists")
	}

	if err := createCurrency(ctx, s.db, currency); err != nil {
		zlog.Error("failed to create currency", zap.Error(err))
		return nil, err
	}

	return currency, nil
}

func (s *Service) UpdateExchangeRate(ctx context.Context, in *ExchangeRateReq) (*Currency, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "UpdateExchangeRate"),
		zap.String("Username", claims.Username),
	)

	if err := in.Validate(); err != nil {
		return nil, err
	}

	currency, err := getCurrency(ctx, s.db, &Query{
		ID: in.ID,
	})
	if errors.Is(err, ErrCurrencyNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this currency or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get currency", zap.Error(err))
		return nil, err
	}

	currency.update(claims.Username, in)
	exists, err := isCurrencyCodeExists(ctx, s.db, currency.ID, currency.Code)
	if err != nil {
		zlog.Error("failed to check if currency exists", zap.Error(err))
		return nil, err
	}
	if exists {
		return nil, rpcStatus.Error(codes.AlreadyExists, "The currency with this code already exists")
	}

	if err := updateCurrency(ctx, s.db, currency); err != nil {
		zlog.Error("failed to update currency", zap.Error(err))
		return nil, err
	}

	return currency, nil
}

func (s *Service) GetCurrencyByID(ctx context.Context, id string) (*Currency, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetCurrencyByID"),
		zap.String("Username", claims.Username),
	)

	currency, err := getCurrency(ctx, s.db, &Query{
		ID: id,
	})
	if errors.Is(err, ErrCurrencyNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this currency or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get currency", zap.Error(err))
		return nil, err
	}

	return currency, nil
}

func (s *Service) GetCurrencyByCode(ctx context.Context, code string) (*Currency, error) {
	claims := auth.ClaimsFromContext(ctx)
	zlog := s.zlog.With(
		zap.String("Method", "GetCurrencyByCode"),
		zap.String("Username", claims.Username),
	)

	currency, err := getCurrency(ctx, s.db, &Query{
		Code: code,
	})
	if errors.Is(err, ErrCurrencyNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this currency or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get currency", zap.Error(err))
		return nil, err
	}

	return currency, nil
}

type ListCurrenciesResult struct {
	Currencies    []*Currency `json:"currencies"`
	NextPageToken string      `json:"nextPageToken"`
}

func (s *Service) ListCurrencies(ctx context.Context, in *Query) (*ListCurrenciesResult, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "ListCurrencies"),
		zap.String("Username", claims.Username),
	)

	currencies, err := listCurrencies(ctx, s.db, in)
	if err != nil {
		zlog.Error("failed to list currencies", zap.Error(err))
		return nil, err
	}

	var pageToken string
	if l := len(currencies); l > 0 && l == int(pager.Size(in.PageSize)) {
		last := currencies[l-1]
		pageToken = pager.EncodeCursor(&pager.Cursor{
			ID:   last.ID,
			Time: last.CreatedAt,
		})
	}

	return &ListCurrenciesResult{
		Currencies:    currencies,
		NextPageToken: pageToken,
	}, nil
}

// Currency represents a currency and its exchange rate.
// The Code is the ISO 4217 currency code.
//
//	Base currency is LAK.
type Currency struct {
	createdBy    string
	updatedBy    string
	ID           string          `json:"id"`
	Code         string          `json:"code"`
	ExchangeRate decimal.Decimal `json:"exchangeRate"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

type Query struct {
	ID            string    `json:"id" param:"id" query:"id"`
	Code          string    `json:"code"  query:"code"`
	PageToken     string    `json:"pageToken"  query:"pageToken"`
	PageSize      uint64    `json:"pageSize"  query:"pageSize"`
	CreatedAfter  time.Time `json:"createdAfter"  query:"createdAfter"`
	CreatedBefore time.Time `json:"createdBefore"  query:"createdBefore"`
}

func (q *Query) ToSql() (string, []any, error) {
	and := sq.And{}

	if q.ID != "" {
		and = append(and, sq.Eq{"id": q.ID})
	}

	if q.Code != "" {
		and = append(and, sq.Eq{"code": q.Code})
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

func listCurrencies(ctx context.Context, db *sql.DB, in *Query) ([]*Currency, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))
	pred, args, err := in.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.
		Select(
			id,
			"code",
			"exchange_rate",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		From(`currency`).
		Where(pred, args...).
		PlaceholderFormat(sq.AtP).
		OrderBy("created_at DESC").
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for listing currencies: %w", err)
	}
	defer rows.Close()

	currencies := make([]*Currency, 0)
	for rows.Next() {
		var c Currency
		err := rows.Scan(
			&c.ID,
			&c.Code,
			&c.ExchangeRate,
			&c.createdBy,
			&c.updatedBy,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCurrencyNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		currencies = append(currencies, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	return currencies, nil
}

func getCurrency(ctx context.Context, db *sql.DB, in *Query) (*Currency, error) {
	in.PageSize = 1

	currencies, err := listCurrencies(ctx, db, in)
	if err != nil {
		return nil, err
	}
	if len(currencies) == 0 {
		return nil, ErrCurrencyNotFound
	}

	return currencies[0], nil
}

func updateCurrency(ctx context.Context, db *sql.DB, in *Currency) error {
	q, args := sq.Update("currency").
		Set("code", in.Code).
		Set("exchange_rate", in.ExchangeRate).
		Set("updated_by", in.updatedBy).
		Set("updated_at", in.UpdatedAt).
		Where(
			sq.Eq{
				"id": in.ID,
			}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("failed to update currency: %w", err)
	}

	return nil
}

func createCurrency(ctx context.Context, db *sql.DB, in *Currency) error {
	q, args := sq.Insert("currency").
		Columns(
			"id",
			"code",
			"exchange_rate",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		Values(
			in.ID,
			in.Code,
			in.ExchangeRate,
			in.createdBy,
			in.updatedBy,
			in.CreatedAt,
			in.UpdatedAt,
		).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("failed to create currency: %w", err)
	}

	return nil
}

// CreateReq represents a request for creating a currency.
type CreateReq struct {
	Code         string          `json:"code"`
	ExchangeRate decimal.Decimal `json:"exchangeRate"`
}

func (r *CreateReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Code == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "code",
			Description: "Code must not be empty",
		})
	}

	valid := countries.CurrencyCodeByName(r.Code)
	if !valid.IsValid() {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "code",
			Description: "Code is not valid. The code must be a valid ISO 4217 currency code",
		})
	}

	if r.ExchangeRate.IsZero() {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "exchangeRate",
			Description: "Exchange rate must not be empty",
		})
	}

	if r.ExchangeRate.LessThan(decimal.Zero) {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "exchangeRate",
			Description: "Exchange rate must be greater than zero",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Currency is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

// ExchangeRateReq represents a request to update an exchange rate.
type ExchangeRateReq struct {
	ID           string          `param:"id"`
	ExchangeRate decimal.Decimal `json:"exchangeRate"`
}

func (r *ExchangeRateReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.ID == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "id",
			Description: "ID must not be empty",
		})
	}

	if r.ExchangeRate.IsZero() {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "exchangeRate",
			Description: "Exchange rate must not be empty",
		})
	}

	if r.ExchangeRate.LessThan(decimal.Zero) {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "exchangeRate",
			Description: "Exchange rate must be greater than zero",
		})
	}

	if len(violations) > 0 {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Exchange rate is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

func newCurrency(createdBy string, in *CreateReq) *Currency {
	now := time.Now()
	return &Currency{
		createdBy:    createdBy,
		updatedBy:    createdBy,
		ID:           gen.ID(),
		Code:         in.Code,
		ExchangeRate: in.ExchangeRate,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func (c *Currency) update(updatedBy string, in *ExchangeRateReq) {
	c.updatedBy = updatedBy
	c.ExchangeRate = in.ExchangeRate
	c.UpdatedAt = time.Now()
}

func isCurrencyCodeExists(ctx context.Context, db *sql.DB, id string, code string) (bool, error) {
	q, args := sq.
		Select("TOP 1 id").
		From("currency").
		Where(
			sq.And{
				sq.NotEq{"id": id},
				sq.Eq{"code": code},
			}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	var idx string
	row := db.QueryRowContext(ctx, q, args...)
	err := row.Scan(&idx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to execute query if currency exists: %w", err)
	}

	return idx != "", nil
}
