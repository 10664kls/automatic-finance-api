package income

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/database"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

// ErrWordlistNotFound is returned when a wordlist is not found in the database.
var ErrWordlistNotFound = errors.New("wordlist not found")

type Wordlist struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Category  source    `json:"category"`
	CreatedBy string    `json:"createdBy"`
	UpdatedBy string    `json:"updatedBy"` // Optional, can be used for updates
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (w *Wordlist) Update(by string, in *WordlistReq) bool {
	w.Word = strings.TrimSpace(in.Word)
	w.Category = in.Category
	w.UpdatedBy = by
	w.UpdatedAt = time.Now()
	return true
}

type ListWordlistsResult struct {
	Wordlists     []*Wordlist `json:"wordlists"`
	NextPageToken string      `json:"nextPageToken"`
}

type WordlistQuery struct {
	noLimit       bool
	ID            int64     `json:"id" param:"id" query:"id"`
	Word          string    `json:"word"  query:"word"`
	Category      string    `json:"category"  query:"category"`
	PageToken     string    `json:"pageToken"  query:"pageToken"`
	PageSize      uint64    `json:"pageSize"  query:"pageSize"`
	CreatedAfter  time.Time `json:"createdAfter"  query:"createdAfter"`
	CreatedBefore time.Time `json:"createdBefore"  query:"createdBefore"`
}

func (q *WordlistQuery) ToSql() (string, []any, error) {
	and := sq.And{}

	if q.ID > 0 {
		and = append(and, sq.Eq{"id": q.ID})
	}

	if q.Word != "" {
		and = append(and, sq.Eq{"word": q.Word})
	}

	if q.Category != "" {
		and = append(and, sq.Eq{"category": q.Category})
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

type WordlistReq struct {
	// ID is used for updating an existing wordlist.
	ID int64 `json:"-" param:"id"`

	Word     string `json:"word"`
	Category source `json:"category"`
}

func (r *WordlistReq) Validate() error {
	violations := make([]*edPb.BadRequest_FieldViolation, 0)

	if r.Word == "" {
		violations = append(violations, &edPb.BadRequest_FieldViolation{
			Field:       "word",
			Description: "Word must not be empty",
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

func (r *WordlistReq) ToWordlist(by string) *Wordlist {
	return &Wordlist{
		ID:        r.ID,
		Word:      strings.TrimSpace(r.Word),
		Category:  r.Category,
		CreatedBy: by,
		CreatedAt: time.Now(),
		UpdatedBy: by,
		UpdatedAt: time.Now(),
	}
}

func matchWordlists(target string, wordlists []*Wordlist) (source, string, bool) {
	target = strings.TrimSpace(target)
	target = strings.ToLower(target)
	for _, w := range wordlists {
		w.Word = strings.TrimSpace(w.Word)
		switch {
		case len(w.Word) <= 3:
			targets := strings.SplitSeq(target, "|")
			for t := range targets {
				t = strings.TrimSpace(t)
				ts := strings.SplitSeq(t, " ")
				for v := range ts {
					v = strings.TrimSpace(v)
					if strings.EqualFold(v, strings.ToLower(w.Word)) {
						return w.Category, w.Word, true
					}
				}
			}

		default:
			if strings.Contains(target, strings.ToLower(w.Word)) {
				return w.Category, w.Word, true
			}
		}
	}

	return SourceUnSpecified, "", false
}

func listWordlists(ctx context.Context, db *sql.DB, in *WordlistQuery) ([]*Wordlist, error) {
	id := fmt.Sprintf("TOP %d id", pager.Size(in.PageSize))
	if in.noLimit {
		id = "id"
	}

	pred, args, err := in.ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.
		Select(
			id,
			"word",
			"category",
			"created_by",
			"created_at",
			"updated_by",
			"updated_at",
		).
		From(`income_wordlist`).
		Where(pred, args...).
		PlaceholderFormat(sq.AtP).
		OrderBy("created_at DESC").
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for listing wordlists: %w", err)
	}
	defer rows.Close()

	wordlists := make([]*Wordlist, 0)

	for rows.Next() {
		var wordlist Wordlist
		err := rows.Scan(
			&wordlist.ID,
			&wordlist.Word,
			&wordlist.Category,
			&wordlist.CreatedBy,
			&wordlist.CreatedAt,
			&wordlist.UpdatedBy,
			&wordlist.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWordlistNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		wordlists = append(wordlists, &wordlist)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	return wordlists, nil
}

func getWordlist(ctx context.Context, db *sql.DB, in *WordlistQuery) (*Wordlist, error) {
	wordlists, err := listWordlists(ctx, db, in)
	if err != nil {
		return nil, err
	}

	if len(wordlists) == 0 {
		return nil, ErrWordlistNotFound
	}
	return wordlists[0], nil
}

func saveWordlist(ctx context.Context, db *sql.DB, wordlist *Wordlist) error {
	return database.WithTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		updatedQuery, args := sq.Update("income_wordlist").
			Set("word", wordlist.Word).
			Set("category", wordlist.Category).
			Set("updated_by", wordlist.UpdatedBy).
			Set("updated_at", wordlist.UpdatedAt).
			Where(sq.Eq{
				"id": wordlist.ID,
			}).
			PlaceholderFormat(sq.AtP).
			MustSql()

		effected, err := tx.ExecContext(ctx, updatedQuery, args...)
		if err != nil {
			return fmt.Errorf("failed to update wordlist: %w", err)
		}

		rowsAffected, err := effected.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			insertQuery, args := sq.Insert("income_wordlist").
				Columns(
					"word",
					"category",
					"created_by",
					"created_at",
					"updated_by",
					"updated_at",
				).
				Values(
					wordlist.Word,
					wordlist.Category,
					wordlist.CreatedBy,
					wordlist.CreatedAt,
					wordlist.UpdatedBy,
					wordlist.UpdatedAt,
				).
				Suffix("SELECT SCOPE_IDENTITY()").
				PlaceholderFormat(sq.AtP).
				MustSql()

			row := tx.QueryRowContext(ctx, insertQuery, args...)
			if err := row.Scan(&wordlist.ID); err != nil {
				return fmt.Errorf("failed to insert wordlist: %w", err)
			}
			return nil
		}

		return nil
	})
}
