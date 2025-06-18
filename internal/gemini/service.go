package gemini

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"google.golang.org/genai"
	"google.golang.org/grpc/codes"
	rpcStatus "google.golang.org/grpc/status"
)

var ErrPromptNotFound = errors.New("prompt not found")

type Service struct {
	model  string
	client *genai.Client
	zlog   *zap.Logger
	db     *sql.DB
}

func NewService(_ context.Context, db *sql.DB, model string, client *genai.Client, zlog *zap.Logger) (*Service, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if client == nil {
		return nil, errors.New("genai client is nil")
	}
	if zlog == nil {
		return nil, errors.New("logger is nil")
	}
	if model == "" {
		return nil, errors.New("model is empty")
	}

	return &Service{
		client: client,
		db:     db,
		zlog:   zlog,
		model:  model,
	}, nil
}

func (s *Service) ExtractCIBLoanContractFromPDF(ctx context.Context, location string) (*CIBInfo, error) {
	zlog := s.zlog.With(
		zap.String("Method", "ExtractCIBLoanContractFromPDF"),
		zap.String("Location", location),
	)

	prompt, err := getPromptByLatestCreatedAt(ctx, s.db)
	if errors.Is(err, ErrPromptNotFound) {
		return nil, rpcStatus.Error(codes.PermissionDenied, "You are not allowed to access this resource or (it may not exist)")
	}
	if err != nil {
		zlog.Error("failed to get prompt", zap.Error(err))
		return nil, err
	}

	file, err := s.client.Files.UploadFromPath(ctx, location, &genai.UploadFileConfig{
		MIMEType: "application/pdf",
	})
	if err != nil {
		zlog.Error("failed to upload file on genai", zap.Error(err))
		return nil, err
	}

	promptParts := []*genai.Part{
		genai.NewPartFromURI(file.URI, file.MIMEType),
		genai.NewPartFromText(prompt),
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(promptParts, genai.RoleUser),
	}

	resp, err := s.client.Models.GenerateContent(
		ctx,
		s.model,
		contents,
		getGenContentConfig(),
	)
	if err != nil {
		zlog.Error("failed to generate content", zap.Error(err))
		return nil, err
	}

	cibInfo := new(CIBInfo)
	if err := json.NewDecoder(bytes.NewBufferString(resp.Text())).Decode(cibInfo); err != nil {
		zlog.Error("failed to decode response", zap.Error(err))
		return nil, err
	}

	return cibInfo, nil
}

func getGenContentConfig() *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"aggregateByBankCodes": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"bankCode": {Type: genai.TypeString},
							"quantity": {Type: genai.TypeInteger},
						},
						PropertyOrdering: []string{
							"bankCode",
							"quantity",
						},
					},
				},
				"account": {
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"displayName": {Type: genai.TypeString},
						"dateOfBirth": {Type: genai.TypeString},
						"phoneNumber": {Type: genai.TypeString},
					},
					PropertyOrdering: []string{
						"displayName",
						"dateOfBirth",
						"phoneNumber",
					},
				},
				"contracts": {
					Type: genai.TypeArray,
					Items: &genai.Schema{
						Type: genai.TypeObject,
						Properties: map[string]*genai.Schema{
							"loanNumber":           {Type: genai.TypeString},
							"lastedAt":             {Type: genai.TypeString},
							"bankCode":             {Type: genai.TypeString},
							"firstInstallmentDate": {Type: genai.TypeString},
							"lastInstallmentDate":  {Type: genai.TypeString},
							"extendDate":           {Type: genai.TypeString},
							"interestRate":         {Type: genai.TypeNumber},
							"typeOfTermLoan":       {Type: genai.TypeString},
							"financeAmount":        {Type: genai.TypeNumber},
							"outstandingBalance":   {Type: genai.TypeNumber},
							"currency":             {Type: genai.TypeString},
							"overdueDay":           {Type: genai.TypeString},
							"gradeCIB":             {Type: genai.TypeString},
							"typeOfLoan":           {Type: genai.TypeString},
							"totalTermLoan":        {Type: genai.TypeString},
							"status":               {Type: genai.TypeString},
							"gradeCIBLast12months": {
								Type: genai.TypeArray,
								Items: &genai.Schema{
									Type: genai.TypeString,
								},
							},
						},
						PropertyOrdering: []string{
							"loanNumber",
							"lastedAt",
							"bankCode",
							"firstInstallmentDate",
							"lastInstallmentDate",
							"extendDate",
							"interestRate",
							"typeOfTermLoan",
							"financeAmount",
							"outstandingBalance",
							"currency",
							"overdueDay",
							"gradeCIB",
							"typeOfLoan",
							"totalTermLoan",
							"status",
							"gradeCIBLast12months",
						},
					},
				},
			},
			PropertyOrdering: []string{
				"contracts",
				"aggregateByBankCodes",
				"account",
			},
		},
	}
}

func getPromptByLatestCreatedAt(ctx context.Context, db *sql.DB) (string, error) {
	q, args := sq.Select("TOP 1 prompt").
		From("genai_prompt").
		PlaceholderFormat(sq.AtP).
		MustSql()

	row := db.QueryRowContext(ctx, q, args...)
	var prompt string
	err := row.Scan(&prompt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPromptNotFound
	}
	if err != nil {
		return "", fmt.Errorf("failed to execute query: %w", err)
	}

	s, err := base64.StdEncoding.DecodeString(prompt)
	if err != nil {
		return "", fmt.Errorf("failed to decode prompt: %w", err)
	}

	return string(s), nil
}

type CIBInfo struct {
	Account              Account               `json:"account"`
	AggregateByBankCodes []AggregateByBankCode `json:"aggregateByBankCodes"`
	Contracts            []Contract            `json:"contracts"`
}

type Account struct {
	DisplayName string `json:"displayName"`
	DateOfBirth string `json:"dateOfBirth"`
	PhoneNumber string `json:"phoneNumber"`
}

type Contract struct {
	LoanNumber           string          `json:"loanNumber"`
	LastedAt             string          `json:"lastedAt"`
	BankCode             string          `json:"bankCode"`
	FirstInstallmentDate string          `json:"firstInstallmentDate"`
	LastInstallmentDate  string          `json:"lastInstallmentDate"`
	InterestRate         decimal.Decimal `json:"interestRate"`
	Type                 string          `json:"typeOfLoan"`
	FinanceAmount        decimal.Decimal `json:"financeAmount"`
	OutstandingBalance   decimal.Decimal `json:"outstandingBalance"`
	Currency             string          `json:"currency"`
	OverdueDay           decimal.Decimal `json:"overdueDay"`
	GradeCIB             string          `json:"gradeCIB"`
	TermType             string          `json:"typeOfTermLoan"`
	Term                 string          `json:"totalTermLoan"`
	Status               string          `json:"status"`
	GradeCIBLast12Months []string        `json:"gradeCIBLast12months"`
}

type AggregateByBankCode struct {
	BankCode string `json:"bankCode"`
	Quantity int64  `json:"quantity"`
}
