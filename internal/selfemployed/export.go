package selfemployed

import (
	"bytes"
	"context"
	"fmt"

	"github.com/xuri/excelize/v2"
)

func (s *Service) exportCalculationsToExcel(ctx context.Context, in *BatchGetCalculationsQuery) (*bytes.Buffer, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheetName = "Calculation of Self-employed"
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
	f.SetCellValue(sheetName, "I1", "Business Segment")
	f.SetCellValue(sheetName, "J1", "Margin Rate")
	f.SetCellStyle(sheetName, "A1", "J1", fontStyle)

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

		f.SetCellValue(sheetName, fmt.Sprintf("I%d", rowNumber), c.BusinessType.Name)
		f.SetCellValue(sheetName, fmt.Sprintf("J%d", rowNumber), fmt.Sprintf("%.2f%%", c.MarginPercentage.InexactFloat64()))
		f.SetCellStyle(sheetName, fmt.Sprintf("J%d", rowNumber), fmt.Sprintf("J%d", rowNumber), numberStyle)
	}
}

func exportCalculationToExcel(calculation *Calculation) (*bytes.Buffer, error) {
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

	setSummaryToExcel(f, numberStyle, fontStyle, sheetName, calculation)
	if err := setMonthlyIncomeToExcel(f, sheetName, fontStyle, numberStyle, calculation); err != nil {
		return nil, fmt.Errorf("failed to set monthly income to excel: %w", err)
	}

	byt, err := f.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("failed to write to buffer: %w", err)
	}

	return byt, nil
}

func setSummaryToExcel(f *excelize.File, numberStyle, fontStyle int, sheetName string, calculation *Calculation) {
	f.MergeCell(sheetName, "B2", "I2")
	f.SetCellValue(sheetName, "B2", "ໃບວິເຄາະສິນເຊື່ອ (ການປະເມີນລາຍໄດ້ຂອງລູກຄ້າ) - ລາຍໄດ້ເຈົ້າຂອງກິດຈະການ")
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

	f.SetCellValue(sheetName, "B7", "ເດືອນ:")

	startRow := 7
	for i, m := range calculation.MonthlyBreakdown.MonthlyIncomes {
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", i+startRow), m.Month)
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", i+startRow), m.Total.InexactFloat64())
		f.MergeCell(sheetName, fmt.Sprintf("D%d", i+startRow), fmt.Sprintf("I%d", i+startRow))
		f.SetCellStyle(sheetName, fmt.Sprintf("C%d", i+startRow), fmt.Sprintf("I%d", 7+i), numberStyle)
	}

	endRow := startRow + len(calculation.MonthlyBreakdown.MonthlyIncomes)
	f.MergeCell(sheetName, fmt.Sprintf("B%d", endRow), fmt.Sprintf("C%d", endRow))
	f.SetCellValue(sheetName, fmt.Sprintf("B%d", endRow), "ຍອດລວມ:")
	f.SetCellStyle(sheetName, fmt.Sprintf("B%d", endRow), fmt.Sprintf("C%d", endRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", endRow), calculation.TotalIncome.InexactFloat64())
	f.MergeCell(sheetName, fmt.Sprintf("D%d", endRow), fmt.Sprintf("I%d", endRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", endRow), fmt.Sprintf("I%d", endRow), numberStyle)

	monthlyStartRow := endRow + 1
	f.MergeCell(sheetName, fmt.Sprintf("B%d", monthlyStartRow), fmt.Sprintf("C%d", monthlyStartRow))
	f.SetCellValue(sheetName, fmt.Sprintf("B%d", monthlyStartRow), fmt.Sprintf("ຍອດສະເລ່ຍ/%d ເດືອນ:", calculation.PeriodInMonth.IntPart()))
	f.SetCellStyle(sheetName, fmt.Sprintf("B%d", monthlyStartRow), fmt.Sprintf("C%d", monthlyStartRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", monthlyStartRow), calculation.MonthlyAverageIncome.InexactFloat64())
	f.MergeCell(sheetName, fmt.Sprintf("D%d", monthlyStartRow), fmt.Sprintf("I%d", monthlyStartRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", monthlyStartRow), fmt.Sprintf("I%d", monthlyStartRow), numberStyle)

	monthlyMarginStartRow := monthlyStartRow + 1
	f.MergeCell(sheetName, fmt.Sprintf("B%d", monthlyMarginStartRow), fmt.Sprintf("C%d", monthlyMarginStartRow))
	f.SetCellValue(sheetName, fmt.Sprintf("B%d", monthlyMarginStartRow), "ຍອດສະເລ່ຍຕໍ່ເດືອນ")
	f.SetCellStyle(sheetName, fmt.Sprintf("B%d", monthlyMarginStartRow), fmt.Sprintf("C%d", monthlyMarginStartRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", monthlyMarginStartRow), calculation.MonthlyAverageByMargin.InexactFloat64())
	f.MergeCell(sheetName, fmt.Sprintf("D%d", monthlyMarginStartRow), fmt.Sprintf("I%d", monthlyMarginStartRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", monthlyMarginStartRow), fmt.Sprintf("I%d", monthlyMarginStartRow), numberStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("J%d", monthlyMarginStartRow), fmt.Sprintf("%.2f%%", calculation.MarginPercentage.InexactFloat64()))
	f.SetCellStyle(sheetName, fmt.Sprintf("J%d", monthlyMarginStartRow), fmt.Sprintf("J%d", monthlyMarginStartRow), numberStyle)

	exchangeRateRow := monthlyMarginStartRow + 1
	f.MergeCell(sheetName, fmt.Sprintf("B%d", exchangeRateRow), fmt.Sprintf("C%d", exchangeRateRow))
	f.SetCellValue(sheetName, fmt.Sprintf("B%d", exchangeRateRow), fmt.Sprintf("ອັດຕາແລກປ່ຽນວັນທີ: %s", calculation.CreatedAt.Format("02/01/2006")))
	f.SetCellStyle(sheetName, fmt.Sprintf("B%d", exchangeRateRow), fmt.Sprintf("C%d", exchangeRateRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", exchangeRateRow), calculation.ExchangeRate.InexactFloat64())
	f.MergeCell(sheetName, fmt.Sprintf("D%d", exchangeRateRow), fmt.Sprintf("I%d", exchangeRateRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", exchangeRateRow), fmt.Sprintf("I%d", exchangeRateRow), numberStyle)

	netIncomeRow := exchangeRateRow + 1
	f.MergeCell(sheetName, fmt.Sprintf("B%d", netIncomeRow), fmt.Sprintf("C%d", netIncomeRow))
	f.SetCellValue(sheetName, fmt.Sprintf("B%d", netIncomeRow), "ຍອດລວມລາຍໄດ້ສຸດທິ:")
	f.SetCellStyle(sheetName, fmt.Sprintf("B%d", netIncomeRow), fmt.Sprintf("C%d", netIncomeRow), fontStyle)

	f.SetCellValue(sheetName, fmt.Sprintf("D%d", netIncomeRow), calculation.MonthlyNetIncome.InexactFloat64())
	f.MergeCell(sheetName, fmt.Sprintf("D%d", netIncomeRow), fmt.Sprintf("I%d", netIncomeRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("D%d", netIncomeRow), fmt.Sprintf("I%d", netIncomeRow), numberStyle)
}

func setMonthlyIncomeToExcel(f *excelize.File, sheetName string, frontStyle, numberStyle int, calculation *Calculation) error {
	startColIDx, err := excelize.ColumnNameToNumber("M")
	if err != nil {
		return fmt.Errorf("failed to convert column name to number: %w", err)
	}

	endRow := findLongestTransactionsFromMonthly(calculation.MonthlyBreakdown) + 5
	for i, m := range calculation.MonthlyBreakdown.MonthlyIncomes {
		colName, err := excelize.ColumnNumberToName(startColIDx + i)
		if err != nil {
			return fmt.Errorf("failed to convert column number to name: %w", err)
		}

		startRow := 2
		f.SetCellValue(sheetName, fmt.Sprintf("%s%d", colName, startRow), m.Month)
		f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", colName, startRow), fmt.Sprintf("%s%d", colName, startRow), frontStyle)

		for i, t := range m.Transactions {
			row := startRow + i + 1

			f.SetCellValue(sheetName, fmt.Sprintf("%s%d", colName, row), t.Amount.InexactFloat64())
			f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", colName, row), fmt.Sprintf("%s%d", colName, row), numberStyle)
		}

		f.SetCellValue(sheetName, fmt.Sprintf("%s%d", colName, endRow), m.Total.InexactFloat64())
		f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", colName, endRow), fmt.Sprintf("%s%d", colName, endRow), numberStyle)
	}

	return nil
}

func findLongestTransactionsFromMonthly(m *MonthlyBreakdown) int {
	if m == nil {
		return 0
	}

	var longest int
	for _, t := range m.MonthlyIncomes {
		if len(t.Transactions) > longest {
			longest = len(t.Transactions)
		}
	}

	return longest
}
