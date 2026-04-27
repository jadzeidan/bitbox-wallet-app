// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"testing"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	accountsmock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/mocks"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/stretchr/testify/require"
)

func TestBalanceAtSnapshotDate(t *testing.T) {
	date1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	account := &accountsmock.InterfaceMock{
		TransactionsFunc: func() (accounts.OrderedTransactions, error) {
			return accounts.OrderedTransactions{
				{
					Height:    2,
					Timestamp: &date2,
					Balance:   coinpkg.NewAmountFromInt64(200),
				},
				{
					Height:    1,
					Timestamp: &date1,
					Balance:   coinpkg.NewAmountFromInt64(100),
				},
			}, nil
		},
	}

	balance, err := balanceAtSnapshotDate(account, time.Date(2025, 1, 1, 23, 59, 59, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, coinpkg.NewAmountFromInt64(100), balance)
}

func TestBalanceAtSnapshotDateMissingConfirmedTimestamp(t *testing.T) {
	account := &accountsmock.InterfaceMock{
		TransactionsFunc: func() (accounts.OrderedTransactions, error) {
			return accounts.OrderedTransactions{
				{
					Height:  1,
					Balance: coinpkg.NewAmountFromInt64(100),
				},
			}, nil
		},
	}

	_, err := balanceAtSnapshotDate(account, time.Now())
	require.ErrorContains(t, err, "timestamp")
}

func TestCreateBalanceStatementPDF(t *testing.T) {
	pdf, err := createBalanceStatementPDF(
		[]statementRow{
			{
				coinName:  "Bitcoin",
				amount:    "1.23456789",
				unit:      "BTC",
				fiatValue: "100'000.00",
			},
		},
		"CHF",
		"100'000.00",
		time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		false,
		false,
	)
	require.NoError(t, err)
	require.NotEmpty(t, pdf)
	require.Contains(t, string(pdf), "%PDF-1.4")
	require.Contains(t, string(pdf), "Statement Of Balance")
	require.Contains(t, string(pdf), "Bitcoin")
}
