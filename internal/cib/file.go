package cib

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
)

var ErrCIBFileNotFound = errors.New("cib file not found")

// ErrUnsupportedFileType is returned when the file type is not supported.
var ErrUnsupportedFileType = errors.New("unsupported file type")

type CIBFile struct {
	ID           int64     `json:"-"`
	Location     string    `json:"-"`
	OriginalName string    `json:"originalName"`
	Name         string    `json:"name"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`

	// Public URL is the signed url to download the file.
	// For output at first time only, not save to DB.
	PublicURL string `json:"publicUrl,omitempty"`
}

func signedURL(f *CIBFile) string {
	toSign := fmt.Sprintf("%d:%s:%s:%d", f.ID, f.Name, f.OriginalName, f.CreatedAt.Unix())
	signed := sha256.Sum256([]byte(toSign))

	return base64.RawURLEncoding.EncodeToString(signed[:])
}

func verifySignature(f *CIBFile, signature string) bool {
	return subtle.ConstantTimeCompare([]byte(signature), []byte(signedURL(f))) == 1
}

func createCIBFile(ctx context.Context, db *sql.DB, in *CIBFile) error {
	q, args := sq.Insert("cib_file").
		Columns(
			"original_file_name",
			"file_name",
			"location",
			"created_by",
			"created_at",
		).
		Values(
			in.OriginalName,
			in.Name,
			in.Location,
			in.CreatedBy,
			in.CreatedAt,
		).
		Suffix("SELECT SCOPE_IDENTITY()").
		PlaceholderFormat(sq.AtP).
		MustSql()

	err := db.QueryRowContext(ctx, q, args...).Scan(&in.ID)
	if err != nil {
		return fmt.Errorf("failed to create cib file: %w", err)
	}

	return nil
}

func getCIBFileByName(ctx context.Context, db *sql.DB, name string) (*CIBFile, error) {
	q, args := sq.Select(
		"TOP 1 id",
		"original_file_name",
		"file_name",
		"location",
		"created_by",
		"created_at",
	).
		From("cib_file").
		Where(sq.Eq{
			"file_name": name,
		}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	f := new(CIBFile)
	row := db.QueryRowContext(ctx, q, args...)
	err := row.Scan(
		&f.ID,
		&f.OriginalName,
		&f.Name,
		&f.Location,
		&f.CreatedBy,
		&f.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCIBFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cib file: %w", err)
	}

	return f, nil
}
