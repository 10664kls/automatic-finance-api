package income

import (
	"bytes"
	"context"
	"fmt"

	"github.com/xuri/excelize/v2"
)

func (s *Service) exportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheetName = "Calculation of Incomes"
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

	f.SetCellValue(sheetName, "A1", "FLAPPL/LO NO")
	f.SetCellValue(sheetName, "B1", "Product")
	f.SetCellValue(sheetName, "C1", "Average income/month")
	f.SetCellValue(sheetName, "D1", "Account Number")
	f.SetCellValue(sheetName, "E1", "Bank Account Name")
	f.SetCellValue(sheetName, "F1", "Period Account")
	f.SetCellValue(sheetName, "G1", "Currency")
	f.SetCellValue(sheetName, "H1", "Net income amount")
	f.SetCellStyle(sheetName, "A1", "H1", fontStyle)

	startRow := 2
	var nextID int64
	for {
		calculations, err := batchGetCalculations(ctx, s.db, 500, nextID, in)
		if err != nil {
			return nil, fmt.Errorf("failed to batch get calculations: %w", err)
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

func exportCalculationToExcel(ctx context.Context, calculation *Calculation) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheetName = "ເງີນເດືອນສະເລ່ຍຫຼາຍເດືອນ"
	sheet, err := f.NewSheet(sheetName)
	if err != nil {
		return nil, fmt.Errorf("failed to create new sheet: %w", err)
	}

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

	f.SetActiveSheet(sheet)
	switch calculation.Product {
	case ProductPL, ProductSF:
		setSummaryToExcelForProductPLAndSF(f, numberStyle, fontStyle, sheetName, calculation)

	case ProductSA:
		setSummaryToExcelForProductSA(f, numberStyle, fontStyle, sheetName, calculation)
	}

	if err := setSalaryToExcel(f, numberStyle, fontStyle, sheetName, calculation); err != nil {
		return nil, fmt.Errorf("failed to set salary to excel: %w", err)
	}
	if err := setAllowanceToExcel(f, numberStyle, fontStyle, sheetName, calculation); err != nil {
		return nil, fmt.Errorf("failed to set allowance to excel: %w", err)
	}
	if err := setCommissionToExcel(f, numberStyle, fontStyle, sheetName, calculation); err != nil {
		return nil, fmt.Errorf("failed to set commission to excel: %w", err)
	}

	byt, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("failed to write to buffer: %w", err)
	}

	return byt, nil
}

func setSummaryToExcelForProductPLAndSF(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) {
	f.MergeCell(sheetName, "B2", "I2")
	f.SetCellValue(sheetName, "B2", "ໃບວິເຄາະສິນເຊື່ອ (ການປະເມີນລາຍໄດ້ຂອງລູກຄ້າ)")
	f.SetCellStyle(sheetName, "B2", "I2", fontStyle)

	f.SetCellValue(sheetName, "B3", "Product")
	f.SetCellValue(sheetName, "C3", calculation.Product)
	f.MergeCell(sheetName, "D3", "I3")
	f.SetCellValue(sheetName, "D3", calculation.Number)
	f.SetCellStyle(sheetName, "B3", "I3", fontStyle)

	f.SetCellValue(sheetName, "B4", "ຊື່ບັນຊີ:")
	f.MergeCell(sheetName, "C4", "I4")
	f.SetCellValue(sheetName, "C4", calculation.Account.DisplayName)
	f.SetCellStyle(sheetName, "B4", "I4", fontStyle)

	f.MergeCell(sheetName, "B5", "I5")
	f.SetCellValue(sheetName, "B5", fmt.Sprintf("ຂໍ້ມູນລາຍການເຄື່ອນໄຫວທາງການເງິນ %s ເດືອນ", calculation.PeriodInMonth.String()))
	f.SetCellStyle(sheetName, "B5", "I5", fontStyle)

	f.SetCellValue(sheetName, "B6", "1.Account No:")
	f.MergeCell(sheetName, "C6", "I6")
	f.SetCellValue(sheetName, "C6", fmt.Sprintf("%s (%s)", calculation.Account.Number, calculation.Account.Currency))
	f.SetCellStyle(sheetName, "B6", "I6", fontStyle)

	f.SetCellValue(sheetName, "B7", "ລວມເງິນເດືອນພື້ນຖານ")
	f.MergeCell(sheetName, "C7", "I7")
	f.SetCellValue(sheetName, "C7", calculation.TotalBasicSalary.InexactFloat64())
	f.SetCellStyle(sheetName, "B7", "I7", numberStyle)

	f.SetCellValue(sheetName, "B8", "ລວມລາຍໄດ້ຈາກບັນຊີ:")
	f.MergeCell(sheetName, "C8", "I8")
	f.SetCellValue(sheetName, "C8", calculation.TotalIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B8", "I8", numberStyle)

	f.SetCellValue(sheetName, "B9", "ສະເລ່ຍລາຍໄດ້ອື່ນ")
	f.MergeCell(sheetName, "C9", "I9")
	f.SetCellValue(sheetName, "C9", calculation.MonthlyOtherIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B9", "I9", numberStyle)

	f.SetCellValue(sheetName, "B10", "ສະເລ່ຍລາຍໄດ້ອື່ນຈາກ: COM/OT")
	f.MergeCell(sheetName, "C10", "I10")
	f.SetCellValue(sheetName, "C10", calculation.Source.Commission.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B10", "I10", numberStyle)

	f.SetCellValue(sheetName, "B11", "ສະເລ່ຍລາຍໄດ້ອື່ນຈາກ: Allowance")
	f.MergeCell(sheetName, "C11", "I11")
	f.SetCellValue(sheetName, "C11", calculation.Source.Allowance.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B11", "I11", numberStyle)

	f.SetCellValue(sheetName, "B12", "ສະເລ່ຍລາຍໄດ້ອື່ນໆ(80%)")
	f.MergeCell(sheetName, "C12", "I12")
	f.SetCellValue(sheetName, "C12", calculation.EightyPercentOfMonthlyOtherIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B12", "I12", numberStyle)

	f.SetCellValue(sheetName, "B13", "ເງິນເດືອນພື້ນຖານ (Min)")
	f.MergeCell(sheetName, "C13", "I13")
	f.SetCellValue(sheetName, "C13", calculation.Source.BasicSalary.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B13", "I13", numberStyle)

	f.SetCellValue(sheetName, "B14", "ເງິນເດືອນພື້ນຖານ (ຕາມສໍາພາດ)")
	f.MergeCell(sheetName, "C14", "I14")
	f.SetCellValue(sheetName, "C14", calculation.BasicSalaryFromInterview.InexactFloat64())
	f.SetCellStyle(sheetName, "B14", "I14", numberStyle)

	f.SetCellValue(sheetName, "B15", "ຍອດສະເລ່ຍລາຍໄດ້/ເດືອນ")
	f.MergeCell(sheetName, "C15", "I15")
	f.SetCellValue(sheetName, "C15", calculation.MonthlyAverageIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B15", "I15", numberStyle)

	f.SetCellValue(sheetName, "B17", "ອັດຕາແລກປ່ຽນວັນທີ")
	f.SetCellValue(sheetName, "C17", calculation.CreatedAt.Format("02/01/2006"))
	f.MergeCell(sheetName, "D17", "I17")
	f.SetCellValue(sheetName, "D17", calculation.ExchangeRate.InexactFloat64())
	f.SetCellStyle(sheetName, "C17", "I17", numberStyle)

	f.SetCellValue(sheetName, "B18", "ຍອດສະເລ່ຍລາຍໄດ້/ເດືອນ (LAK)")
	f.SetCellStyle(sheetName, "B18", "B18", fontStyle)

	f.MergeCell(sheetName, "C18", "I18")
	f.SetCellValue(sheetName, "C18", calculation.MonthlyNetIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "C18", "I18", numberStyle)

}

func setSummaryToExcelForProductSA(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) {
	f.MergeCell(sheetName, "B2", "I2")
	f.SetCellValue(sheetName, "B2", "ໃບວິເຄາະສິນເຊື່ອ (ການປະເມີນລາຍໄດ້ຂອງລູກຄ້າ)")
	f.SetCellStyle(sheetName, "B2", "I2", fontStyle)

	f.SetCellValue(sheetName, "B3", "Product")
	f.SetCellValue(sheetName, "C3", calculation.Product)
	f.MergeCell(sheetName, "D3", "I3")
	f.SetCellValue(sheetName, "D3", calculation.Number)
	f.SetCellStyle(sheetName, "B3", "I3", fontStyle)

	f.SetCellValue(sheetName, "B4", "ຊື່ບັນຊີ:")
	f.MergeCell(sheetName, "C4", "I4")
	f.SetCellValue(sheetName, "C4", calculation.Account.DisplayName)
	f.SetCellStyle(sheetName, "B4", "I4", fontStyle)

	f.MergeCell(sheetName, "B5", "I5")
	f.SetCellValue(sheetName, "B5", fmt.Sprintf("ຂໍ້ມູນລາຍການເຄື່ອນໄຫວທາງການເງິນ %s ເດືອນ", calculation.PeriodInMonth.String()))
	f.SetCellStyle(sheetName, "B5", "I5", fontStyle)

	f.SetCellValue(sheetName, "B6", "1.Account No:")
	f.MergeCell(sheetName, "C6", "I6")
	f.SetCellValue(sheetName, "C6", fmt.Sprintf("%s (%s)", calculation.Account.Number, calculation.Account.Currency))
	f.SetCellStyle(sheetName, "B6", "I6", fontStyle)

	f.SetCellValue(sheetName, "B7", "ລວມລາຍໄດ້ຈາກບັນຊີ:")
	f.MergeCell(sheetName, "C7", "I7")
	f.SetCellValue(sheetName, "C7", calculation.TotalIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B7", "I7", numberStyle)

	f.SetCellValue(sheetName, "B8", "ເງິນເດືອນພື້ນຖານ")
	f.MergeCell(sheetName, "C8", "I8")
	f.SetCellValue(sheetName, "C8", calculation.Source.BasicSalary.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B8", "I8", numberStyle)

	f.SetCellValue(sheetName, "B9", "ເງິນເດືອນພື້ນຖານ (ຕາມສໍາພາດ)")
	f.MergeCell(sheetName, "C9", "I9")
	f.SetCellValue(sheetName, "C9", calculation.BasicSalaryFromInterview.InexactFloat64())
	f.SetCellStyle(sheetName, "B9", "I9", numberStyle)

	f.SetCellValue(sheetName, "B10", "ສະເລ່ຍລາຍໄດ້ອື່ນຈາກ: COM/OT")
	f.MergeCell(sheetName, "C10", "I10")
	f.SetCellValue(sheetName, "C10", calculation.Source.Commission.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B10", "I10", numberStyle)

	f.SetCellValue(sheetName, "B11", "ສະເລ່ຍລາຍໄດ້ອື່ນຈາກ: Allowance")
	f.MergeCell(sheetName, "C11", "I11")
	f.SetCellValue(sheetName, "C11", calculation.Source.Allowance.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, "B11", "I11", numberStyle)

	f.SetCellValue(sheetName, "B12", "ຍອດສະເລ່ຍລາຍໄດ້/ເດືອນ")
	f.MergeCell(sheetName, "C12", "I12")
	f.SetCellValue(sheetName, "C12", calculation.MonthlyAverageIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "B12", "I12", numberStyle)

	f.SetCellValue(sheetName, "B14", "ອັດຕາແລກປ່ຽນວັນທີ")
	f.SetCellValue(sheetName, "C14", calculation.CreatedAt.Format("02/01/2006"))
	f.MergeCell(sheetName, "D14", "I14")
	f.SetCellValue(sheetName, "D14", calculation.ExchangeRate.InexactFloat64())
	f.SetCellStyle(sheetName, "C14", "I14", numberStyle)

	f.SetCellValue(sheetName, "B15", "ຍອດສະເລ່ຍລາຍໄດ້/ເດືອນ (LAK)")
	f.SetCellStyle(sheetName, "B15", "B15", fontStyle)

	f.MergeCell(sheetName, "C15", "I15")
	f.SetCellValue(sheetName, "C15", calculation.MonthlyNetIncome.InexactFloat64())
	f.SetCellStyle(sheetName, "C15", "I15", numberStyle)
}

func setSalaryToExcel(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) error {
	f.SetCellValue(sheetName, "L3", "Fixed Income salary (Only)")
	f.MergeCell(sheetName, "L3", "M3")
	f.SetCellStyle(sheetName, "L3", "M3", fontStyle)

	f.SetCellValue(sheetName, "L4", "Month")
	f.SetCellValue(sheetName, "M4", "Total Salary/Month")
	f.SetCellStyle(sheetName, "L4", "M4", fontStyle)

	f.SetCellValue(sheetName, "N3", "ຈຳນວນຄັ້ງທີ່ເງີນເດືອນເຂົ້າ")
	f.SetCellStyle(sheetName, "N3", "N3", fontStyle)
	longestReceived := findSalaryLongestTimesReceived(calculation)
	if err := mergeFromCol(f, sheetName, "N", 3, longestReceived); err != nil {
		return err
	}

	titles := listTitleForTimesReceived(longestReceived)
	if err := setStringsAcrossExcelCols(f, sheetName, "N", 4, titles); err != nil {
		return err
	}

	startRow := 5
	for i, v := range calculation.SalaryBreakdown.MonthlySalaries {
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow+i), v.Month)
		f.SetCellValue(sheetName, fmt.Sprintf("M%d", startRow+i), v.Total.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("M%d", startRow+i), fmt.Sprintf("M%d", startRow+i), numberStyle)

		amounts := listNumberFromTransactions(v.Transactions)
		if err := setNumbersAcrossExcelCols(f, sheetName, "N", startRow+i, numberStyle, amounts); err != nil {
			return err
		}
	}

	endRow := len(calculation.SalaryBreakdown.MonthlySalaries) + startRow
	rowTitle := fmt.Sprintf("L%d", endRow)
	rowAmount := fmt.Sprintf("M%d", endRow)
	f.SetCellValue(sheetName, rowTitle, "Total")
	f.SetCellStyle(sheetName, rowTitle, rowTitle, fontStyle)

	f.SetCellValue(sheetName, rowAmount, calculation.TotalIncome.InexactFloat64())
	f.SetCellStyle(sheetName, rowAmount, rowAmount, numberStyle)

	return nil
}

func setAllowanceToExcel(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) error {
	startRow := 10 + len(calculation.SalaryBreakdown.MonthlySalaries)

	f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow), "Allowance")
	f.MergeCell(sheetName, fmt.Sprintf("L%d", startRow), fmt.Sprintf("M%d", startRow))

	f.SetCellValue(sheetName, fmt.Sprintf("N%d", startRow), "ຫານ (ເດືອນ)")
	f.SetCellValue(sheetName, fmt.Sprintf("O%d", startRow), "ສະເລ່ຍ/ເດືອນ")
	f.SetCellStyle(sheetName, fmt.Sprintf("L%d", startRow), fmt.Sprintf("O%d", startRow), fontStyle)

	for i, v := range calculation.AllowanceBreakdown.Allowances {
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow+i+1), v.Title)
		f.SetCellValue(sheetName, fmt.Sprintf("M%d", startRow+i+1), v.Total.InexactFloat64())
		f.SetCellValue(sheetName, fmt.Sprintf("N%d", startRow+i+1), v.Months.InexactFloat64())
		f.SetCellValue(sheetName, fmt.Sprintf("O%d", startRow+i+1), v.MonthlyAverage.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("M%d", startRow+i+1), fmt.Sprintf("O%d", startRow+i+1), numberStyle)
	}

	endRow := startRow + len(calculation.AllowanceBreakdown.Allowances) + 1
	f.SetCellValue(sheetName, fmt.Sprintf("L%d", endRow), "ລວມລາຍຮັບອື່ນໆທັງໝົດ")
	f.MergeCell(sheetName, fmt.Sprintf("L%d", endRow), fmt.Sprintf("N%d", endRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("L%d", endRow), fmt.Sprintf("N%d", endRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("O%d", endRow), calculation.Source.Allowance.Total.InexactFloat64())
	f.SetCellStyle(sheetName, fmt.Sprintf("O%d", endRow), fmt.Sprintf("O%d", endRow), numberStyle)
	return nil
}

func setCommissionToExcel(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) error {
	startRow := 16 + len(calculation.SalaryBreakdown.MonthlySalaries) + len(calculation.AllowanceBreakdown.Allowances)

	f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow), "commission/OT(Monthly income)")
	f.MergeCell(sheetName, fmt.Sprintf("L%d", startRow), fmt.Sprintf("M%d", startRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("L%d", startRow), fmt.Sprintf("M%d", startRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("L%d", startRow+1), "Month")
	f.SetCellValue(sheetName, fmt.Sprintf("M%d", startRow+1), "Total/Month")
	f.SetCellStyle(sheetName, fmt.Sprintf("L%d", startRow+1), fmt.Sprintf("M%d", startRow+1), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("N%d", startRow), "ຈຳນວນຄັ້ງທີ່ເງີນເດືອນເຂົ້າ")
	f.SetCellStyle(sheetName, fmt.Sprintf("N%d", startRow), fmt.Sprintf("N%d", startRow), fontStyle)

	longestReceived := findCommissionLongestTimesReceived(calculation)
	if err := mergeFromCol(f, sheetName, "N", startRow, longestReceived); err != nil {
		return err
	}

	titles := listTitleForTimesReceived(longestReceived)
	if err := setStringsAcrossExcelCols(f, sheetName, "N", startRow+1, titles); err != nil {
		return err
	}

	rowNumber := startRow + 2
	for i, v := range calculation.CommissionBreakdown.Commissions {
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", rowNumber+i), v.Month)
		f.SetCellValue(sheetName, fmt.Sprintf("M%d", rowNumber+i), v.Total.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("M%d", rowNumber+i), fmt.Sprintf("M%d", rowNumber+i), numberStyle)

		amounts := listNumberFromTransactions(v.Transactions)
		if err := setNumbersAcrossExcelCols(f, sheetName, "N", rowNumber+i, numberStyle, amounts); err != nil {
			return err
		}
	}

	endRow := len(calculation.CommissionBreakdown.Commissions) + rowNumber
	rowTitle := fmt.Sprintf("L%d", endRow)
	rowAmount := fmt.Sprintf("M%d", endRow)
	f.SetCellValue(sheetName, rowTitle, "Average")
	f.SetCellStyle(sheetName, rowTitle, rowTitle, fontStyle)

	f.SetCellValue(sheetName, rowAmount, calculation.Source.Commission.MonthlyAverage.InexactFloat64())
	f.SetCellStyle(sheetName, rowAmount, rowAmount, numberStyle)

	return nil
}

func findSalaryLongestTimesReceived(calculation *Calculation) int {
	l := 1
	for _, v := range calculation.SalaryBreakdown.MonthlySalaries {
		if len(v.Transactions) > l {
			l = len(v.Transactions)
		}
	}
	return l
}

func findCommissionLongestTimesReceived(calculation *Calculation) int {
	l := 1
	for _, v := range calculation.CommissionBreakdown.Commissions {
		if len(v.Transactions) > l {
			l = len(v.Transactions)
		}
	}
	return l
}

func listTitleForTimesReceived(l int) []string {
	var headers []string
	for i := 1; i <= l; i++ {
		headers = append(headers, fmt.Sprintf("ຄັ້ງທີ %d", i))
	}

	return headers
}

func listNumberFromTransactions(ts []Transaction) []float64 {
	numbers := make([]float64, 0)
	for _, v := range ts {
		numbers = append(numbers, v.Amount.InexactFloat64())
	}
	return numbers
}

func mergeFromCol(f *excelize.File, sheet string, startCol string, row int, numCols int) error {
	colIdx, err := excelize.ColumnNameToNumber(startCol)
	if err != nil {
		return err
	}

	endColIdx := colIdx + numCols - 1
	endCol, err := excelize.ColumnNumberToName(endColIdx)
	if err != nil {
		return err
	}

	startCell := startCol + fmt.Sprintf("%d", row)
	endCell := endCol + fmt.Sprintf("%d", row)

	return f.MergeCell(sheet, startCell, endCell)
}

func setStringsAcrossExcelCols(f *excelize.File, sheet string, startCol string, row int, values []string) error {
	colIdx, err := excelize.ColumnNameToNumber(startCol)
	if err != nil {
		return err
	}

	for i, val := range values {
		colName, err := excelize.ColumnNumberToName(colIdx + i)
		if err != nil {
			return err
		}
		cell := fmt.Sprintf("%s%d", colName, row)
		f.SetCellValue(sheet, cell, val)
	}

	return nil
}

func setNumbersAcrossExcelCols(f *excelize.File, sheet string, startCol string, row int, style int, values []float64) error {
	colIdx, err := excelize.ColumnNameToNumber(startCol)
	if err != nil {
		return err
	}

	for i, val := range values {
		colName, err := excelize.ColumnNumberToName(colIdx + i)
		if err != nil {
			return err
		}
		cell := fmt.Sprintf("%s%d", colName, row)
		f.SetCellValue(sheet, cell, val)
		f.SetCellStyle(sheet, cell, cell, style)
	}

	return nil
}

func setCalculationsToExcel(f *excelize.File, sheetName string, numberStyle int, startRow int, calculations []*Calculation) {
	for i, c := range calculations {
		rowNumber := startRow + i
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowNumber), c.Number)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowNumber), c.Product)

		f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowNumber), c.MonthlyAverageIncome.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("C%d", rowNumber), fmt.Sprintf("C%d", rowNumber), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowNumber), c.Account.Number)
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowNumber), c.Account.DisplayName)
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowNumber), c.PeriodInMonth.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("F%d", rowNumber), fmt.Sprintf("F%d", rowNumber), numberStyle)

		f.SetCellValue(sheetName, fmt.Sprintf("G%d", rowNumber), c.Account.Currency)

		f.SetCellValue(sheetName, fmt.Sprintf("H%d", rowNumber), c.MonthlyNetIncome.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("H%d", rowNumber), fmt.Sprintf("H%d", rowNumber), numberStyle)
	}
}
