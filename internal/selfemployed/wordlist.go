package selfemployed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/database"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	sq "github.com/Masterminds/squirrel"
	edpb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcstatus "google.golang.org/grpc/status"
)

// ErrWordlistNotFound is returned when a wordlist is not found in the database.
var ErrWordlistNotFound = errors.New("wordlist not found")

func matchWordlist(target string, wordlists []*Wordlist) bool {
	target = strings.TrimSpace(target)
	target = strings.ToLower(target)

	for _, w := range wordlists {
		w.Word = strings.TrimSpace(w.Word)
		if strings.Contains(target, strings.ToLower(w.Word)) {
			return true
		}
	}

	return false
}

type Wordlist struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	CreatedBy string    `json:"createdBy"`
	UpdatedBy string    `json:"updatedBy"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type WordlistReq struct {
	// ID is used for updating an existing wordlist.
	ID int64 `json:"-" param:"id"`

	Word string `json:"word"`
}

func (r *WordlistReq) Validate() error {
	violations := make([]*edpb.BadRequest_FieldViolation, 0)

	if r.Word == "" {
		violations = append(violations, &edpb.BadRequest_FieldViolation{
			Field:       "word",
			Description: "Word must not be empty",
		})
	}

	r.Word = strings.TrimSpace(r.Word)
	r.Word = html.EscapeString(r.Word)

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

// ToWordlist converts the given request to a new Wordlist.
func (r *WordlistReq) ToWordlist(by string) *Wordlist {
	return &Wordlist{
		ID:        r.ID,
		Word:      r.Word,
		CreatedBy: by,
		CreatedAt: time.Now(),
		UpdatedBy: by,
		UpdatedAt: time.Now(),
	}
}

func (w *Wordlist) update(by string, word string) {
	w.Word = word
	w.UpdatedBy = by
	w.UpdatedAt = time.Now()
}

type WordlistQuery struct {
	noLimit       bool
	ID            int64     `json:"id" param:"id" query:"id"`
	Word          string    `json:"word"  query:"word"`
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

func saveWordlist(ctx context.Context, db *sql.DB, in *Wordlist) error {
	return database.WithTx(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		updatedQuery, args := sq.Update("self_employed_wordlist").
			Set("word", in.Word).
			Set("updated_by", in.UpdatedBy).
			Set("updated_at", in.UpdatedAt).
			Where(sq.Eq{
				"id": in.ID,
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
			insertQuery, args := sq.Insert("self_employed_wordlist").
				Columns(
					"word",
					"created_by",
					"created_at",
					"updated_by",
					"updated_at",
				).
				Values(
					in.Word,
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
				return fmt.Errorf("failed to insert wordlist: %w", err)
			}

			return nil
		}

		return nil
	})
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
			"created_by",
			"created_at",
			"updated_by",
			"updated_at",
		).
		From(`self_employed_wordlist`).
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
		w := new(Wordlist)
		err := rows.Scan(
			&w.ID,
			&w.Word,
			&w.CreatedBy,
			&w.CreatedAt,
			&w.UpdatedBy,
			&w.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWordlistNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		wordlists = append(wordlists, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate rows: %w", err)
	}

	return wordlists, nil
}

func getWordlist(ctx context.Context, db *sql.DB, in *WordlistQuery) (*Wordlist, error) {
	in.PageSize = 1
	if in.ID == 0 && in.Word == "" {
		return nil, ErrWordlistNotFound
	}

	wordlists, err := listWordlists(ctx, db, in)
	if err != nil {
		return nil, err
	}

	if len(wordlists) == 0 {
		return nil, ErrWordlistNotFound
	}
	return wordlists[0], nil
}

func isWordlistExists(ctx context.Context, db *sql.DB, in *WordlistReq) (bool, error) {
	q, args := sq.Select("TOP 1 id").
		From("self_employed_wordlist").
		Where(sq.And{
			sq.Eq{
				"word": in.Word,
			},
			sq.NotEq{
				"id": in.ID,
			},
		}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	var id int64
	err := db.QueryRowContext(ctx, q, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return id > 0, nil
}
