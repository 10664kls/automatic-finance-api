package cib

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/shopspring/decimal"
)

var ErrUnableToExtractPDF = errors.New("unable to extract pdf")

func (s *Service) extractPDF(ctx context.Context, in *CIBFile) (*CreditBureau, error) {
	f, err := os.ReadFile(in.Location)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	b64 := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(f)
	type reqBody struct {
		CIB struct {
			Base64 string `json:"base64"`
		} `json:"cib"`
	}

	byt, err := json.Marshal(reqBody{
		CIB: struct {
			Base64 string `json:"base64"`
		}{
			Base64: b64,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.pdfExtractorURL, bytes.NewBuffer(byt))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnableToExtractPDF
	}

	var r responseExtracted
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return mapExtractedData(&r), nil
}

type responseExtracted struct {
	Extraction struct {
		FirstNameEn  string        `json:"first_name_en"`
		LastNameEn   string        `json:"last_name_en"`
		MobileNumber string        `json:"mobile_no"`
		DOB          string        `json:"birth_date"`
		Histories    []loanHistory `json:"histories"`
		Actives      []loanActive  `json:"actives"`
	} `json:"extracted_data"`
}

func mapExtractedData(e *responseExtracted) *CreditBureau {
	aggregateMap := make(map[string]decimal.Decimal, 0)
	cibGradesMap := make(map[string][]string, 0)
	for _, a := range e.Extraction.Actives {
		cibGradesMap[a.AccountNumber] = extractCIBGradeToStringArray(a.PerfHistory12Mths)
	}

	defaultGrade := make([]string, 0)
	for i, c := range e.Extraction.Histories {
		if _, ok := aggregateMap[c.BankNameEn]; !ok {
			aggregateMap[c.BankNameEn] = decimal.NewFromInt(0)
		}
		aggregateMap[c.BankNameEn] = aggregateMap[c.BankNameEn].Add(decimal.NewFromInt(1))

		e.Extraction.Histories[i].GradeCIBLast12Months = defaultGrade
		if val, ok := cibGradesMap[c.AccountNumber]; ok {
			e.Extraction.Histories[i].GradeCIBLast12Months = val
		}
	}

	as := make([]AggregateByBankCode, 0)
	for k, v := range aggregateMap {
		as = append(as, AggregateByBankCode{
			BankCode: k,
			Quantity: v,
		})
	}

	return &CreditBureau{
		DisplayName:         fmt.Sprintf("%s %s", e.Extraction.FirstNameEn, e.Extraction.LastNameEn),
		MobileNumber:        e.Extraction.MobileNumber,
		DOB:                 e.Extraction.DOB,
		Contracts:           e.Extraction.Histories,
		AggregateByBankCode: as,
	}
}

func extractCIBGradeToStringArray(s string) []string {
	hs := make([]string, 0)
	for _, h := range s {
		hs = append(hs, string(h))
	}
	return hs
}

type CreditBureau struct {
	DisplayName         string        `json:"displayName"`
	MobileNumber        string        `json:"mobileNumber"`
	DOB                 string        `json:"dob"`
	Contracts           []loanHistory `json:"contracts"`
	AggregateByBankCode []AggregateByBankCode
}

type loanActive struct {
	AccountNumber     string `json:"account_number"`
	UpdateDate        string `json:"update_date"`
	BankName          string `json:"bank_name"`
	ContractDate      string `json:"contract_date"`
	CreditLimit       string `json:"credit_limit"`
	OsBalance         string `json:"os_balance"`
	Currency          string `json:"currency"`
	NoOfOverdueDays   string `json:"no_of_overdue_days"`
	PerfHistory12Mths string `json:"perf_history_12mths"`
}

type loanHistory struct {
	AccountNumber        string   `json:"account_number"`
	RecentDate           string   `json:"recent_date"`
	BankNameEn           string   `json:"bank_name_en"`
	OpenedDate           string   `json:"opened_date"`
	MatureDate           string   `json:"mature_date"`
	Interest             string   `json:"interest"`
	TypeOfProduct        string   `json:"type_of_product"`
	CreditLimit          string   `json:"credit_limit"`
	OsBalance            string   `json:"os_balance"`
	Currency             string   `json:"currency"`
	NoOfOverdueDays      string   `json:"no_of_overdue_days"`
	DelinquencyCode      string   `json:"delinquency_code"`
	TypeOfLoan           string   `json:"type_of_loan"`
	Tenor                string   `json:"tenor"`
	AccountStatusEng     string   `json:"account_status_eng"`
	GradeCIBLast12Months []string `json:"grade_cib_last_12months"`
}
