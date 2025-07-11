package cib

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/10664kls/automatic-finance-api/internal/auth"
	"github.com/10664kls/automatic-finance-api/internal/currency"
	"github.com/10664kls/automatic-finance-api/internal/pager"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
	"go.uber.org/zap"
	edPb "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

type Service struct {
	pdfExtractorURL string
	db              *sql.DB
	mu              *sync.Mutex
	currency        *currency.Service
	zlog            *zap.Logger
}

func NewService(_ context.Context, db *sql.DB, currency *currency.Service, zlog *zap.Logger, pdfExtractorURL string) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}
	if currency == nil {
		return nil, errors.New("currency service is nil")
	}
	if pdfExtractorURL == "" {
		return nil, errors.New("pdf extractor url is empty")
	}

	return &Service{
		db:              db,
		currency:        currency,
		pdfExtractorURL: pdfExtractorURL,
		mu:              new(sync.Mutex),
		zlog:            zlog,
	}, nil
}

type CIBFileReq struct {
	OriginalName string
	ReadSeeker   io.ReadSeeker
}

func (s *Service) UploadCIB(ctx context.Context, in *CIBFileReq) (*CIBFile, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "UploadCIB"),
		zap.String("Username", claims.Username),
		zap.String("OriginalName", in.OriginalName),
	)

	mime, err := mimetype.DetectReader(in.ReadSeeker)
	if err != nil {
		zlog.Error("failed to detect file type", zap.Error(err))
		return nil, err
	}

	// allow only pdf files
	switch mime.String() {
	case "application/pdf":

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFileType, mime.String())
	}

	if _, err := in.ReadSeeker.Seek(0, io.SeekStart); err != nil {
		zlog.Error("failed to seek file", zap.Error(err))
		return nil, err
	}

	name := uuid.NewString() + mime.Extension()
	location := filepath.Join(os.Getenv("ASSETS_PATH"), "assets", "cib", name)
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

	cibFile := &CIBFile{
		Name:         name,
		OriginalName: in.OriginalName,
		Location:     location,
		CreatedBy:    claims.Username,
		CreatedAt:    time.Now(),
	}

	if err := createCIBFile(ctx, s.db, cibFile); err != nil {
		zlog.Error("failed to create cib file", zap.Error(err))
		return nil, err
	}

	return cibFile, nil
}

func (s *Service) GetCIBFile(ctx context.Context, fileName string, signature string) (*CIBFile, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetCIBFile"),
		zap.String("Username", claims.Username),
		zap.String("fileName", fileName),
	)

	cibFile, err := getCIBFileByName(ctx, s.db, fileName)
	if errors.Is(err, ErrCIBFileNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this file.")
	}
	if err != nil {
		zlog.Error("failed to get cib file", zap.Error(err))
		return nil, err
	}

	if !verifySignature(cibFile, signature) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this file.")
	}

	return cibFile, nil
}

func (s *Service) CalculateCIB(ctx context.Context, in *CalculateReq) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "CalculateCIB"),
		zap.String("Username", claims.Username),
		zap.Any("req", in),
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

	cibFile, err := getCIBFileByName(ctx, s.db, in.CIBFileName)
	if errors.Is(err, ErrCIBFileNotFound) {
		s, _ := rpcStatus.New(
			codes.InvalidArgument,
			"Calculation is not valid or incomplete. Please check the errors and try again, see details for more information.",
		).WithDetails(&edPb.BadRequest{
			FieldViolations: []*edPb.BadRequest_FieldViolation{
				{
					Field:       "cibFileName",
					Description: "CIB file must be a valid file name",
				},
			},
		})

		return nil, s.Err()
	}
	if err != nil {
		zlog.Error("failed to get cib file", zap.Error(err))
		return nil, err
	}

	extraction, err := s.extractPDF(ctx, cibFile)
	if err != nil {
		zlog.Error("failed to extract pdf", zap.Error(err))
		return nil, err
	}

	currencies, err := s.currency.ListCurrencies(ctx, &currency.Query{
		PageSize: 200,
	})
	if err != nil {
		return nil, err
	}

	calculation := newCalculationFromCIBInfo(claims.Username, in.Number, cibFile.Name, extraction, currencies.Currencies)
	if err := saveCalculation(ctx, s.db, calculation); err != nil {
		zlog.Error("failed to create calculation", zap.Error(err))
		return nil, err
	}

	return calculation, nil
}

func (s *Service) GetCalculationByNumber(ctx context.Context, number string) (*Calculation, error) {
	claims := auth.ClaimsFromContext(ctx)

	zlog := s.zlog.With(
		zap.String("Method", "GetCalculationCIBByNumber"),
		zap.String("Username", claims.Username),
		zap.String("number", number),
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

func (s *Service) SignedURL(ctx context.Context, in *CIBFile) string {
	return fmt.Sprintf("%s/v1/files/%s?signature=%s", os.Getenv("BACKEND_URL"), in.Name, signedURL(in))
}

func (s *Service) ExportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	claims := auth.ClaimsFromContext(ctx)
	zlog := s.zlog.With(
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

	buf, err := s.exportCalculationToExcel(ctx, calculation)
	if err != nil {
		zlog.Error("failed to export calculation to excel", zap.Error(err))
		return nil, err
	}

	return buf, nil
}
