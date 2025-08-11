package selfemployed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/gen"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	edpb "google.golang.org/genproto/googleapis/rpc/errdetails"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	rpcstatus "google.golang.org/grpc/status"
)

// ErrBusinessNotFound is returned when a business is not found.
var ErrBusinessNotFound = errors.New("business not found")

type Business struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	MarginPercentage decimal.Decimal `json:"marginPercentage"`
	CreatedBy        string          `json:"createdBy"`
	UpdatedBy        string          `json:"updatedBy"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

func (b *Business) update(by string, name string, description string, marginPercentage decimal.Decimal) {
	b.Name = name
	b.Description = description
	b.MarginPercentage = marginPercentage
	b.UpdatedBy = by
	b.UpdatedAt = time.Now()
}

func newBusiness(by string, name string, description string, marginPercentage decimal.Decimal) *Business {
	now := time.Now()

	return &Business{
		ID:               gen.ID(),
		Name:             name,
		Description:      description,
		MarginPercentage: marginPercentage,
		CreatedBy:        by,
		UpdatedBy:        by,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

type BusinessReq struct {
	ID               string          `json:"-" param:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	MarginPercentage decimal.Decimal `json:"marginPercentage"`
}

func (r *BusinessReq) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Name == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "name",
			Description: "Name must not be empty",
		})
	}
	if r.MarginPercentage.IsZero() {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "marginPercentage",
			Description: "Margin percentage must not be zero",
		})
	}

	if r.MarginPercentage.LessThan(decimal.Zero) {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "marginPercentage",
			Description: "Margin percentage must not be negative",
		})
	}

	if r.MarginPercentage.GreaterThan(decimal.NewFromInt(100)) {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "marginPercentage",
			Description: "Margin percentage must not be greater than 100",
		})
	}

	r.Description = html.EscapeString(strings.TrimSpace(r.Description))

	if len(violations) > 0 {
		s, _ := rpcstatus.New(
			codes.InvalidArgument,
			"Business is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edpb.BadRequest{
			FieldViolations: violations,
		})

		return s.Err()
	}

	return nil
}

type BusinessQuery struct {
	ID            string    `query:"id"`
	Name          string    `query:"name"`
	CreatedAfter  time.Time `query:"createdAfter"`
	CreatedBefore time.Time `query:"createdBefore"`
	PageSize      uint64    `query:"pageSize"`
	PageToken     string    `query:"pageToken"`
}

func (q *BusinessQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != "" {
		and = append(and, sq.Eq{"id": q.ID})
	}
	if q.Name != "" {
		q.Name = html.EscapeString(strings.TrimSpace(q.Name))
		and = append(and, sq.Expr("name LIKE ?", "%"+q.Name+"%"))
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

func createBusiness(ctx context.Context, db *sql.DB, in *Business) error {
	q, args := sq.Insert("business_type").
		Columns(
			"id",
			"name",
			"description",
			"margin_percentage",
			"created_by",
			"updated_by",
			"created_at",
			"updated_at",
		).
		Values(
			in.ID,
			in.Name,
			in.Description,
			in.MarginPercentage,
			in.CreatedBy,
			in.UpdatedBy,
			in.CreatedAt,
			in.UpdatedAt,
		).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}

	return nil
}

func updateBusiness(ctx context.Context, db *sql.DB, in *Business) error {
	q, args := sq.Update("business_type").
		Set("name", in.Name).
		Set("description", in.Description).
		Set("margin_percentage", in.MarginPercentage).
		Set("updated_by", in.UpdatedBy).
		Set("updated_at", in.UpdatedAt).
		Where(sq.Eq{"id": in.ID}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	_, err := db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}

	return nil
}

func listBusinesses(ctx context.Context, db *sql.DB, in *BusinessQuery) ([]*Business, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))

	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.Select(
		id,
		"name",
		"description",
		"margin_percentage",
		"created_by",
		"updated_by",
		"created_at",
		"updated_at",
	).
		From("business_type").
		Where(pred, args...).
		OrderBy("created_at DESC").
		PlaceholderFormat(sq.AtP).
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list businesses: %w", err)
	}
	defer rows.Close()

	businesses := make([]*Business, 0)
	for rows.Next() {
		b := new(Business)
		err := rows.Scan(
			&b.ID,
			&b.Name,
			&b.Description,
			&b.MarginPercentage,
			&b.CreatedBy,
			&b.UpdatedBy,
			&b.CreatedAt,
			&b.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBusinessNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan business: %w", err)
		}

		businesses = append(businesses, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate over businesses: %w", err)
	}

	return businesses, nil
}

func getBusiness(ctx context.Context, db *sql.DB, in *BusinessQuery) (*Business, error) {
	in.PageSize = 1

	if in.ID == "" && in.Name == "" {
		return nil, ErrBusinessNotFound
	}

	businesses, err := listBusinesses(ctx, db, in)
	if err != nil {
		return nil, err
	}

	if len(businesses) == 0 {
		return nil, ErrBusinessNotFound
	}

	return businesses[0], nil
}

func isBusinessExists(ctx context.Context, db *sql.DB, in *BusinessReq) (bool, error) {
	q, args := sq.Select("TOP 1 id").
		From("business_type").
		Where(sq.And{
			sq.Eq{
				"name": in.Name,
			},
			sq.NotEq{
				"id": in.ID,
			},
		}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	row := db.QueryRowContext(ctx, q, args...)
	var id string
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check if business exists: %w", err)
	}

	return id != "", nil
}
