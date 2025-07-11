package cib

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"
)

func (s *Service) exportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheetName = "Calculation of cib"
	sheet, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to create new sheet: %w", err)
	}
	f.SetActiveSheet(sheet)

	formatNumber := "#,##0.00"
	numberStyle, err := f.NewStyle(&excelize.Style{
		CustomNumFmt: &formatNumber,
		Font: &excelize.Font{
			Bold: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create style: %w", err)
	}

	fontStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create front style: %w", err)
	}

	f.SetCellValue(sheetName, "A1", "Enter Facility / LO Number")
	f.SetCellValue(sheetName, "B1", "Customer Name")
	f.SetCellValue(sheetName, "C1", "Total Loan")
	f.SetCellValue(sheetName, "D1", "Total closed loan")
	f.SetCellValue(sheetName, "E1", "Total active loan")
	f.SetCellValue(sheetName, "F1", "Total installment (CIB)")
	f.SetCellStyle(sheetName, "A1", "F1", fontStyle)

	startRow := 2
	var nextID int64
	for {
		calculations, err := batchGetCalculations(ctx, s.db, 500, nextID, in)
		if err != nil {
			return nil, fmt.Errorf("failed to get calculations: %w", err)
		}

		if len(calculations) == 0 {
			break
		}

		s.mu.Lock()
		nextID = calculations[len(calculations)-1].ID
		s.mu.Unlock()

		setCalculationsToExcel(f, sheetName, numberStyle, startRow, calculations)

		startRow += len(calculations)
	}

	byt, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("failed to write to buffer: %w", err)
	}

	return byt, nil
}

func (s *Service) exportCalculationToExcel(ctx context.Context, calculation *Calculation) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	formatNumber := "#,##0.00"
	numberStyle, err := f.NewStyle(&excelize.Style{
		CustomNumFmt: &formatNumber,
		Font: &excelize.Font{
			Bold: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create style: %w", err)
	}

	fontStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{
			Bold: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create front style: %w", err)
	}

	setCalculationToSummaryExcelSheet(ctx, f, fontStyle, numberStyle, calculation.TotalInstallmentInLAK, calculation.Contracts)
	setCalculationToActiveLoanExcelSheet(ctx, f, fontStyle, numberStyle, calculation.Contracts)
	setCalculationToClosedLoanExcelSheet(ctx, f, fontStyle, numberStyle, calculation.Contracts)

	byt, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("failed to write to buffer: %w", err)
	}

	return byt, nil
}

func setCalculationToSummaryExcelSheet(_ context.Context, f *excelize.File, fontStyle int, numberStyle int, totalInstallmentInLak decimal.Decimal, contracts []Contract) error {
	const sheetName = "Summary all Loan"
	summarySheet, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create new sheet: %w", err)
	}

	f.SetActiveSheet(summarySheet)

	f.SetCellValue(sheetName, "A1", "ທະນາຄານ")
	f.SetCellValue(sheetName, "B1", "ວັນທີເຊັນສັນຍາ")
	f.SetCellValue(sheetName, "C1", "ວັນທີໝົດສັນຍາ")
	f.SetCellValue(sheetName, "D1", "ອັດຕາດອກເບ້ຍ")
	f.SetCellValue(sheetName, "E1", "ເປົ້າໝາຍເງິນກູ້/ປະເພດບັດສິນເຊື່ອ")
	f.SetCellValue(sheetName, "F1", "ວົງເງິນອະນຸມັດກູ້")
	f.SetCellValue(sheetName, "G1", "ຍອດເຫຼືອໜີ້")
	f.SetCellValue(sheetName, "H1", "ສະກຸນເງິນ")
	f.SetCellValue(sheetName, "I1", "ຈຳນວນວັນທີ່ຊຳລະຊ້າ")
	f.SetCellValue(sheetName, "J1", "ການຈັດຊັ້ນໜີ້")
	f.SetCellValue(sheetName, "K1", "ປະເພດເງິນກູ້")
	f.SetCellValue(sheetName, "L1", "ໄລຍະເວລາໃນການກູ້ຢືມ")
	f.SetCellValue(sheetName, "M1", "ສະຖານະພາບ")
	f.SetCellValue(sheetName, "N1", "term")
	f.SetCellValue(sheetName, "O1", "Installment by currency")
	f.SetCellValue(sheetName, "P1", "InstLAK")
	f.SetCellStyle(sheetName, "A1", "P1", fontStyle)

	startRow := 2

	for i, contract := range contracts {
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow+i), contract.BankCode)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", startRow+i), contract.FirstInstallment)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", startRow+i), contract.LastInstallment)

		f.SetCellValue(sheetName, fmt.Sprintf("D%d", startRow+i), contract.InterestRate.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("D%d", startRow+i), fmt.Sprintf("D%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("E%d", startRow+i), contract.Type)

		f.SetCellValue(sheetName, fmt.Sprintf("F%d", startRow+i), contract.FinanceAmount.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("F%d", startRow+i), fmt.Sprintf("F%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("G%d", startRow+i), contract.OutstandingBalance.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("G%d", startRow+i), fmt.Sprintf("G%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("H%d", startRow+i), contract.Currency)

		f.SetCellValue(sheetName, fmt.Sprintf("I%d", startRow+i), contract.OverdueInDay.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("I%d", startRow+i), fmt.Sprintf("I%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("J%d", startRow+i), contract.GradeCIB)
		f.SetCellValue(sheetName, fmt.Sprintf("K%d", startRow+i), contract.TermType)
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow+i), contract.Term)
		f.SetCellValue(sheetName, fmt.Sprintf("M%d", startRow+i), contract.Status)

		f.SetCellValue(sheetName, fmt.Sprintf("N%d", startRow+i), contract.Period.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("N%d", startRow+i), fmt.Sprintf("N%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("O%d", startRow+i), contract.Installment.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("O%d", startRow+i), fmt.Sprintf("O%d", startRow+i), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("P%d", startRow+i), contract.InstallmentInLAK.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("P%d", startRow+i), fmt.Sprintf("P%d", startRow+i), numberStyle)
	}

	endRow := len(contracts) + startRow
	f.SetCellValue(sheetName, fmt.Sprintf("P%d", endRow), totalInstallmentInLak.InexactFloat64())
	f.SetCellStyle(sheetName, fmt.Sprintf("P%d", endRow), fmt.Sprintf("P%d", endRow), numberStyle)

	return nil
}

func setCalculationToActiveLoanExcelSheet(_ context.Context, f *excelize.File, fontStyle int, numberStyle int, contracts []Contract) error {
	const sheetName = "Active Loan"
	activeLoanSheet, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create new sheet: %w", err)
	}

	f.SetActiveSheet(activeLoanSheet)

	f.SetCellValue(sheetName, "A1", "ທະນາຄານ")
	f.SetCellValue(sheetName, "B1", "ວົງເງິນອະນຸມັດກູ້")
	f.SetCellValue(sheetName, "C1", "ຍອດເຫຼືອໜີ້")
	f.SetCellValue(sheetName, "D1", "ການຈັດຊັ້ນໜີ້")
	f.SetCellValue(sheetName, "E1", "term")
	f.SetCellValue(sheetName, "F1", "Installment")
	f.SetCellValue(sheetName, "G1", "InstLAK")
	f.SetCellValue(sheetName, "H1", "ປະຫວັດການຈັດຊັ້ນໜີ້12ເດືອນນັບຈາກເດືອນປະຈຸບັນ")
	f.SetCellStyle(sheetName, "A1", "H1", fontStyle)
	f.MergeCell(sheetName, "H1", "S1")

	startRow := 2
	var totalInstallmentInLak decimal.Decimal
	var last12Months []string = []string{"H", "I", "J", "K", "L", "M", "N", "O", "P", "Q", "R", "S"}
	for _, c := range contracts {
		if c.Status != StatusActive {
			continue
		}

		f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), c.BankCode)

		f.SetCellValue(sheetName, fmt.Sprintf("B%d", startRow), c.FinanceAmount.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("B%d", startRow), fmt.Sprintf("B%d", startRow), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("C%d", startRow), c.OutstandingBalance.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("C%d", startRow), fmt.Sprintf("C%d", startRow), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("D%d", startRow), c.GradeCIB)

		f.SetCellValue(sheetName, fmt.Sprintf("E%d", startRow), c.Period.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("E%d", startRow), fmt.Sprintf("E%d", startRow), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("F%d", startRow), c.Installment.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("F%d", startRow), fmt.Sprintf("F%d", startRow), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("G%d", startRow), c.InstallmentInLAK.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("G%d", startRow), fmt.Sprintf("G%d", startRow), numberStyle)

		for i, grade := range c.GradeCIBLast12Months {
			f.SetCellValue(sheetName, fmt.Sprintf("%s%d", last12Months[i], startRow), grade)
		}
		totalInstallmentInLak = totalInstallmentInLak.Add(c.InstallmentInLAK)

		startRow++
	}

	f.SetCellValue(sheetName, fmt.Sprintf("G%d", startRow), totalInstallmentInLak.InexactFloat64())
	f.SetCellStyle(sheetName, fmt.Sprintf("G%d", startRow), fmt.Sprintf("G%d", startRow), numberStyle)

	return nil
}

func setCalculationToClosedLoanExcelSheet(_ context.Context, f *excelize.File, fontStyle int, numberStyle int, contracts []Contract) error {
	const sheetName = "Closed Loan"
	closedLoanSheet, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create new sheet: %w", err)
	}

	f.SetActiveSheet(closedLoanSheet)

	f.SetCellValue(sheetName, "A1", "ທະນາຄານ")
	f.SetCellValue(sheetName, "B1", "ວົງເງິນອະນຸມັດກູ້")
	f.SetCellValue(sheetName, "C1", "ການຈັດຊັ້ນໜີ້")
	f.SetCellStyle(sheetName, "A1", "C1", fontStyle)

	startRow := 2
	for _, c := range contracts {
		if c.Status != StatusClosed {
			continue
		}

		f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), c.BankCode)

		f.SetCellValue(sheetName, fmt.Sprintf("B%d", startRow), c.FinanceAmount.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("B%d", startRow), fmt.Sprintf("B%d", startRow), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("C%d", startRow), c.GradeCIB)
		startRow++
	}

	return nil
}

func setCalculationsToExcel(f *excelize.File, sheetName string, numberStyle int, startRow int, calculations []*Calculation) {
	for i, c := range calculations {
		rowNumber := startRow + i
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowNumber), c.Number)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowNumber), c.Customer.DisplayName)

		f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowNumber), c.AggregateQuantity.Total.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("C%d", rowNumber), fmt.Sprintf("C%d", rowNumber), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowNumber), c.AggregateQuantity.Closed.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("D%d", rowNumber), fmt.Sprintf("D%d", rowNumber), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowNumber), c.AggregateQuantity.Active.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("E%d", rowNumber), fmt.Sprintf("E%d", rowNumber), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowNumber), c.TotalInstallmentInLAK.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("F%d", rowNumber), fmt.Sprintf("F%d", rowNumber), numberStyle)
	}
}

type BatchGetCalculationsQuery struct {
	ID                  int64     `query:"id"`
	Number              string    `query:"number"`
	CustomerDisplayName string    `query:"customer"`
	CreatedAfter        time.Time `query:"createdAfter"`
	CreatedBefore       time.Time `query:"createdBefore"`

	nextID int64
}

func (q *BatchGetCalculationsQuery) ToSQL() (string, []any, error) {
	and := sq.And{}
	if q.ID != 0 {
		and = append(and, sq.Eq{"id": q.ID})
	}
	if q.Number != "" {
		and = append(and, sq.Expr("number LIKE ?", "%"+q.Number+"%"))
	}
	if q.CustomerDisplayName != "" {
		and = append(and, sq.Expr("customer_display_name LIKE ?", "%"+q.CustomerDisplayName+"%"))
	}

	if !q.CreatedAfter.IsZero() {
		and = append(and, sq.GtOrEq{"created_at": q.CreatedAfter})
	}

	if !q.CreatedBefore.IsZero() {
		and = append(and, sq.LtOrEq{"created_at": q.CreatedBefore})
	}

	if q.nextID > 0 {
		and = append(and, sq.Lt{"id": q.nextID})
	}

	return and.ToSql()
}

func batchGetCalculations(ctx context.Context, db *sql.DB, batchSize int, nextID int64, in *BatchGetCalculationsQuery) ([]*Calculation, error) {
	id := fmt.Sprintf("TOP %d id", batchSize)
	in.nextID = nextID
	pred, args, err := in.ToSQL()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	q, args := sq.
		Select(
			id,
			"number",
			"cib_file_name",
			"customer_display_name",
			"customer_phone_number",
			"customer_dob",
			"total_loan",
			"total_closed_loan",
			"total_active_loan",
			"total_installment_lak",
			"aggregate_by_bank",
			"contract_info",
			"created_by",
			"created_at",
			"updated_by",
			"updated_at",
		).
		From(`cib_file_analysis`).
		Where(pred, args...).
		PlaceholderFormat(sq.AtP).
		OrderBy("id DESC").
		MustSql()

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for listing calculations: %w", err)
	}
	defer rows.Close()

	calculations := make([]*Calculation, 0)
	for rows.Next() {
		var c Calculation
		var contracts, aggregateBank []byte
		err := rows.Scan(
			&c.ID,
			&c.Number,
			&c.CIBFileName,
			&c.Customer.DisplayName,
			&c.Customer.PhoneNumber,
			&c.Customer.DateOfBirth,
			&c.AggregateQuantity.Total,
			&c.AggregateQuantity.Closed,
			&c.AggregateQuantity.Active,
			&c.TotalInstallmentInLAK,
			&aggregateBank,
			&contracts,
			&c.CreatedBy,
			&c.CreatedAt,
			&c.UpdatedBy,
			&c.UpdatedAt,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCalculationNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to scan calculation: %w", err)
		}

		if err := json.Unmarshal(contracts, &c.Contracts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contracts: %w", err)
		}

		banks := make([]AggregateByBankCode, 0)
		if err := json.Unmarshal(aggregateBank, &banks); err != nil {
			return nil, fmt.Errorf("failed to unmarshal aggregate by bank: %w", err)
		}

		c.AggregateByBankCode = banks
		calculations = append(calculations, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate calculations: %w", err)
	}

	return calculations, nil
}
