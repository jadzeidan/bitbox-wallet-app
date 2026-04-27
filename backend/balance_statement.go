// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	utilcfg "github.com/BitBoxSwiss/bitbox-wallet-app/util/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
)

type statementRow struct {
	coinName         string
	amount           string
	unit             string
	fiatValue        string
	hasEstimatedFiat bool
}

// ExportBalanceStatement exports a PDF statement for the selected active accounts
// at the provided snapshot date.
func (backend *Backend) ExportBalanceStatement(
	accountCodes []accountsTypes.Code,
	snapshotDate time.Time,
) error {
	accountsByCode := map[accountsTypes.Code]accounts.Interface{}
	for _, account := range backend.Accounts() {
		config := account.Config().Config
		if config.Inactive || config.HiddenBecauseUnused {
			continue
		}
		accountsByCode[config.Code] = account
	}

	selectedAccounts := make([]accounts.Interface, 0, len(accountCodes))
	seen := make(map[accountsTypes.Code]struct{}, len(accountCodes))
	for _, accountCode := range accountCodes {
		if _, exists := seen[accountCode]; exists {
			continue
		}
		seen[accountCode] = struct{}{}
		account, ok := accountsByCode[accountCode]
		if !ok {
			return errp.Newf("account %s is not active", accountCode)
		}
		if account.FatalError() {
			return errp.Newf("account %s is not available", accountCode)
		}
		selectedAccounts = append(selectedAccounts, account)
	}
	if len(selectedAccounts) == 0 {
		return errp.New("no accounts selected")
	}

	snapshotStart := time.Date(
		snapshotDate.Year(),
		snapshotDate.Month(),
		snapshotDate.Day(),
		0, 0, 0, 0,
		snapshotDate.Location(),
	)
	snapshotEnd := snapshotStart.AddDate(0, 0, 1).Add(-time.Nanosecond)
	fiat := backend.Config().AppConfig().Backend.MainFiat

	totalByCoin := make(map[coinpkg.Code]*big.Int)
	for _, account := range selectedAccounts {
		if err := account.Initialize(); err != nil {
			return err
		}
		balanceAtDate, err := balanceAtSnapshotDate(account, snapshotEnd)
		if err != nil {
			return err
		}
		coinCode := account.Coin().Code()
		if _, exists := totalByCoin[coinCode]; !exists {
			totalByCoin[coinCode] = big.NewInt(0)
		}
		totalByCoin[coinCode].Add(totalByCoin[coinCode], balanceAtDate.BigInt())
	}

	rows := make([]statementRow, 0, len(totalByCoin))
	totalFiat := new(big.Rat)
	hasMissingFiat := false
	hasEstimatedFiat := false
	for coinCode, totalAmountInt := range totalByCoin {
		coin, err := backend.Coin(coinCode)
		if err != nil {
			return err
		}
		totalAmount := coinpkg.NewAmount(totalAmountInt)
		row := statementRow{
			coinName: coin.Name(),
			amount:   coin.FormatAmount(totalAmount, false),
			unit:     coin.GetFormatUnit(false),
		}

		priceAtSnapshot := backend.RatesUpdater().HistoricalPriceAt(string(coinCode), fiat, snapshotEnd)
		amountRat := new(big.Rat).SetFrac(totalAmountInt, coinpkg.DecimalsExp(coin, false))
		switch {
		case priceAtSnapshot > 0:
			fiatValue := new(big.Rat).Mul(amountRat, new(big.Rat).SetFloat64(priceAtSnapshot))
			row.fiatValue = coinpkg.FormatAsCurrency(fiatValue, fiat)
			totalFiat.Add(totalFiat, fiatValue)
		case snapshotEnd.Before(time.Now()) && time.Since(snapshotEnd) < 2*time.Hour:
			latestPrice, err := backend.RatesUpdater().LatestPriceForPair(coin.Unit(false), fiat)
			if err == nil && latestPrice > 0 {
				fiatValue := new(big.Rat).Mul(amountRat, new(big.Rat).SetFloat64(latestPrice))
				row.fiatValue = coinpkg.FormatAsCurrency(fiatValue, fiat)
				row.hasEstimatedFiat = true
				totalFiat.Add(totalFiat, fiatValue)
				hasEstimatedFiat = true
				break
			}
			row.fiatValue = "N/A"
			hasMissingFiat = true
		default:
			row.fiatValue = "N/A"
			hasMissingFiat = true
		}

		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].coinName < rows[j].coinName })
	totalFiatLabel := coinpkg.FormatAsCurrency(totalFiat, fiat)
	if hasMissingFiat {
		totalFiatLabel = "N/A"
	}

	pdfBytes, err := createBalanceStatementPDF(
		rows,
		fiat,
		totalFiatLabel,
		snapshotStart,
		hasMissingFiat,
		hasEstimatedFiat,
	)
	if err != nil {
		return err
	}

	exportsDir, err := utilcfg.ExportsDir()
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("%s-balance-statement.pdf", time.Now().Format("2006-01-02-at-15-04-05"))
	suggestedPath := filepath.Join(exportsDir, filename)
	path := backend.Environment().GetSaveFilename(suggestedPath)
	if path == "" {
		return errp.ErrUserAbort
	}
	if err := os.WriteFile(path, pdfBytes, 0o644); err != nil {
		return err
	}
	// Open the generated PDF for immediate feedback to the user.
	if err := backend.environment.SystemOpen(path); err != nil {
		// On mobile we expect this to work; on desktop it should also work in normal environments.
		// Keep this as a hard error so the frontend can surface the issue.
		return err
	}
	return nil
}

func balanceAtSnapshotDate(
	account accounts.Interface,
	snapshotEnd time.Time,
) (coinpkg.Amount, error) {
	txs, err := account.Transactions()
	if err != nil {
		return coinpkg.Amount{}, err
	}
	for _, tx := range txs {
		if tx.Height <= 0 {
			continue
		}
		if tx.Timestamp == nil {
			return coinpkg.Amount{}, errp.New("confirmed transaction timestamp unavailable")
		}
		if tx.Timestamp.After(snapshotEnd) {
			continue
		}
		return tx.Balance, nil
	}
	return coinpkg.NewAmountFromInt64(0), nil
}

func createBalanceStatementPDF(
	rows []statementRow,
	fiat string,
	totalFiat string,
	snapshotDate time.Time,
	hasMissingFiat bool,
	hasEstimatedFiat bool,
) ([]byte, error) {
	const (
		pageWidth   = 595.0
		pageHeight  = 842.0
		marginLeft  = 40.0
		marginRight = 555.0
	)
	coinColumn := 46.0
	amountColumn := 250.0
	fiatColumn := 430.0

	var pages []string
	var ops []string
	addOp := func(s string) { ops = append(ops, s) }
	flushPage := func() {
		pages = append(pages, strings.Join(ops, "\n"))
		ops = []string{"0.5 w"}
	}
	newPage := func(withMainHeader bool) float64 {
		ops = []string{"0.5 w"}
		y := 800.0
		if withMainHeader {
			addOp(pdfText(40, y, 18, "Statement Of Balance"))
			y -= 26
			addOp(pdfText(40, y, 11, fmt.Sprintf("Snapshot Date: %s", snapshotDate.Format("2006-01-02"))))
			y -= 30
		} else {
			addOp(pdfText(40, y, 14, "Statement Of Balance (Continued)"))
			y -= 24
		}
		addOp(pdfLine(marginLeft, y+10, marginRight, y+10))
		addOp(pdfText(coinColumn, y-2, 12, "Coin"))
		addOp(pdfText(amountColumn, y-2, 12, "Amount"))
		addOp(pdfText(fiatColumn, y-2, 12, fmt.Sprintf("Value (%s)", fiat)))
		addOp(pdfLine(marginLeft, y-10, marginRight, y-10))
		return y - 28
	}

	y := newPage(true)
	for _, row := range rows {
		if y < 90 {
			flushPage()
			y = newPage(false)
		}
		addOp(pdfText(coinColumn, y, 11, row.coinName))
		addOp(pdfText(amountColumn, y, 11, fmt.Sprintf("%s %s", row.amount, row.unit)))
		addOp(pdfText(fiatColumn, y, 11, row.fiatValue))
		addOp(pdfLine(marginLeft, y-8, marginRight, y-8))
		y -= 20
	}

	if y < 100 {
		flushPage()
		y = newPage(false)
	}
	addOp(pdfLine(marginLeft, y-4, marginRight, y-4))
	addOp(pdfText(coinColumn, y-20, 12, "Total"))
	addOp(pdfText(fiatColumn, y-20, 12, totalFiat))
	addOp(pdfLine(marginLeft, y-28, marginRight, y-28))
	y -= 50

	if hasMissingFiat {
		addOp(pdfText(coinColumn, y, 10, "Note: Some fiat values were unavailable for the selected snapshot date and are shown as N/A."))
		y -= 16
	}
	if hasEstimatedFiat {
		addOp(pdfText(coinColumn, y, 10, "Note: Some fiat values use latest rates because historical rates were not yet available."))
	}

	flushPage()
	return buildSimplePDF(pages, pageWidth, pageHeight)
}

func buildSimplePDF(pageContents []string, pageWidth, pageHeight float64) ([]byte, error) {
	if len(pageContents) == 0 {
		return nil, errp.New("cannot build PDF without pages")
	}

	objectCount := 3 + len(pageContents)*2
	objects := make([]string, objectCount+1)

	objects[1] = "<< /Type /Catalog /Pages 2 0 R >>"

	pageRefs := make([]string, 0, len(pageContents))
	for i := range pageContents {
		pageObjID := 4 + i*2
		pageRefs = append(pageRefs, fmt.Sprintf("%d 0 R", pageObjID))
	}
	objects[2] = fmt.Sprintf(
		"<< /Type /Pages /Kids [%s] /Count %d >>",
		strings.Join(pageRefs, " "),
		len(pageContents),
	)
	objects[3] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	for i, content := range pageContents {
		pageObjID := 4 + i*2
		contentObjID := pageObjID + 1
		objects[pageObjID] = fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>",
			pageWidth, pageHeight, contentObjID,
		)
		objects[contentObjID] = fmt.Sprintf(
			"<< /Length %d >>\nstream\n%s\nendstream",
			len([]byte(content)),
			content,
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	offsets := make([]int, objectCount+1)
	for objectID := 1; objectID <= objectCount; objectID++ {
		offsets[objectID] = buf.Len()
		_, _ = fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", objectID, objects[objectID])
	}

	xrefOffset := buf.Len()
	_, _ = fmt.Fprintf(&buf, "xref\n0 %d\n", objectCount+1)
	buf.WriteString("0000000000 65535 f \n")
	for objectID := 1; objectID <= objectCount; objectID++ {
		_, _ = fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[objectID])
	}
	_, _ = fmt.Fprintf(
		&buf,
		"trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		objectCount+1,
		xrefOffset,
	)
	return buf.Bytes(), nil
}

func pdfText(x, y, size float64, text string) string {
	return fmt.Sprintf(
		"BT /F1 %.2f Tf 1 0 0 1 %.2f %.2f Tm (%s) Tj ET",
		size,
		x,
		y,
		pdfEscape(text),
	)
}

func pdfLine(x1, y1, x2, y2 float64) string {
	return fmt.Sprintf("%.2f %.2f m %.2f %.2f l S", x1, y1, x2, y2)
}

func pdfEscape(input string) string {
	var b strings.Builder
	for _, r := range input {
		if r < 32 || r > 126 {
			r = '?'
		}
		switch r {
		case '\\', '(', ')':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
