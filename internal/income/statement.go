package income

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	sq "github.com/Masterminds/squirrel"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrUnsupportedFileType is returned when the file type is not supported.
var ErrUnsupportedFileType = errors.New("unsupported file type")

// ErrStatementFileNotFound is returned when a statement file is not found in the database.
var ErrStatementFileNotFound = errors.New("statement file not found")

type StatementFile struct {
	ID           int64     `json:"-"`
	Location     string    `json:"-"`
	OriginalName string    `json:"originalName"`
	Name         string    `json:"name"`
	CreatedBy    string    `json:"createdBy"`
	CreatedAt    time.Time `json:"createdAt"`

	// Public URL is the signed url to download the file.
	// For output at first time only, not save to DB.
	PublicURL string `json:"publicUrl"`
}

func (s *Service) SignedURL(ctx context.Context, in *StatementFile) string {
	return fmt.Sprintf("%s/v1/files/%s?signature=%s", os.Getenv("BACKEND_URL"), in.Name, signedURL(in))
}

type FileStatementReq struct {
	OriginalName string
	ReadSeeker   io.ReadSeeker
}

func (s *Service) UploadStatement(ctx context.Context, in *FileStatementReq) (*StatementFile, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "UploadStatement"),
		zap.String("Username", claims.Username),
	)

	mime, err := mimetype.DetectReader(in.ReadSeeker)
	if err != nil {
		zlog.Error("failed to detect file type", zap.Error(err))
		return nil, err
	}

	// allow only excel files
	switch mime.String() {
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFileType, mime.String())
	}

	if _, err := in.ReadSeeker.Seek(0, io.SeekStart); err != nil {
		zlog.Error("failed to seek file", zap.Error(err))
		return nil, err
	}

	exePath, err := os.Executable()
	if err != nil {
		zlog.Error("failed to get executable path", zap.Error(err))
		return nil, err
	}

	name := uuid.NewString() + mime.Extension()
	baseDir := filepath.Dir(exePath)
	location := filepath.Join(baseDir, "assets", "statement", name)
	dst, err := os.OpenFile(location, os.O_CREATE|os.O_APPEND|os.O_RDWR, os.ModePerm)
	if err != nil {
		zlog.Error("failed to open file", zap.Error(err))
		return nil, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, in.ReadSeeker); err != nil {
		zlog.Error("failed to copy file", zap.Error(err))
		return nil, err
	}

	statement := &StatementFile{
		Name:         name,
		OriginalName: in.OriginalName,
		CreatedBy:    claims.Username,
		Location:     location,
		CreatedAt:    time.Now(),
	}

	if err := createStatementFile(ctx, s.db, statement); err != nil {
		zlog.Error("failed to create statement file", zap.Error(err))
		return nil, err
	}

	return statement, nil
}

func (s *Service) GetStatement(ctx context.Context, name string, signature string) (*StatementFile, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetStatement"),
		zap.String("Username", claims.Username),
	)

	statementFile, err := getStatementFileByName(ctx, s.db, name)
	if errors.Is(err, ErrStatementFileNotFound) {
		return nil, status.Error(codes.PermissionDenied, "You are not allowed to access this file.")
	}
	if err != nil {
		zlog.Error("failed to get statement file", zap.Error(err))
		return nil, err
	}

	if !verifySignature(statementFile, signature) {
		return nil, status.Error(codes.PermissionDenied, "You are not allowed to access this file.")
	}

	return statementFile, nil
}

func signedURL(f *StatementFile) string {
	toSign := fmt.Sprintf("%d:%s:%s:%d", f.ID, f.Name, f.OriginalName, f.CreatedAt.Unix())
	signed := sha256.Sum256([]byte(toSign))

	return base64.RawURLEncoding.EncodeToString(signed[:])
}

func verifySignature(f *StatementFile, signature string) bool {
	return subtle.ConstantTimeCompare([]byte(signature), []byte(signedURL(f))) == 1
}

func createStatementFile(ctx context.Context, db *sql.DB, in *StatementFile) error {
	q, args := sq.Insert("statement_file").
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
		return fmt.Errorf("failed to create statement file: %w", err)
	}

	return nil
}

func getStatementFileByName(ctx context.Context, db *sql.DB, name string) (*StatementFile, error) {
	q, args := sq.Select(
		"TOP 1 id",
		"original_file_name",
		"file_name",
		"location",
		"created_by",
		"created_at",
	).
		From("statement_file").
		Where(sq.Eq{
			"file_name": name,
		}).
		PlaceholderFormat(sq.AtP).
		MustSql()

	f := new(StatementFile)
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
		return nil, ErrStatementFileNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get statement file: %w", err)
	}

	return f, nil
}
