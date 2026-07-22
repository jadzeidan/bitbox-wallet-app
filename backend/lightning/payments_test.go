// SPDX-License-Identifier: Apache-2.0

package lightning

import (
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	accountErrors "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/errors"
	btccoin "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/rates"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/socksproxy"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/stretchr/testify/require"
)

func makeTestLightning() *Lightning {
	return &Lightning{
		btcCoin: btccoin.NewCoin(
			coin.CodeBTC,
			"Bitcoin",
			"BTC",
			coin.BtcUnitDefault,
			&chaincfg.MainNetParams,
			".",
			[]*config.ServerInfo{},
			"",
			"",
			socksproxy.NewSocksProxy(false, ""),
		),
		ratesUpdater: rates.NewRateUpdater(nil, os.DevNull),
	}
}

// makeActiveLightning returns a lightning instance with a configured account and formatting
// helpers, ready to receive wallet state pushes.
func makeActiveLightning(t *testing.T) *Lightning {
	t.Helper()

	coinLightning := makeTestLightning()
	lightning := newTestLightning(t, nil)
	lightning.btcCoin = coinLightning.btcCoin
	lightning.ratesUpdater = coinLightning.ratesUpdater
	require.NoError(t, lightning.SetAccount(&config.LightningAccountConfig{
		Seed:            "test mnemonic",
		RootFingerprint: []byte{0xde, 0xad, 0xbe, 0xef},
		Code:            "v0-deadbeef-ln-0",
		Number:          0,
	}))
	return lightning
}

func TestToLightningPayment(t *testing.T) {
	lightning := makeTestLightning()

	payment := lightning.toLightningPayment(walletPayment{
		ID:        "payment-id",
		Kind:      paymentKindReceive,
		Status:    paymentStatusComplete,
		AmountSat: 123,
		FeeSat:    4,
		CreatedAt: 42,
		Note:      "invoice description",
		Invoice:   "lnbc1invoice",
	})

	require.Equal(t, lightningPayment{
		ID:          "payment-id",
		Type:        accounts.TxTypeReceive,
		Status:      accounts.TxStatusComplete,
		Time:        stringPointer("1970-01-01T00:00:42Z"),
		Description: "invoice description",
		Amount: coinAmountWithConversions(
			"0.00000123",
		),
		AmountAtTime: coinAmountWithConversions(
			"0.00000123",
		),
		DeductedAmountAtTime: coinAmountWithConversions(
			"0.00000000",
		),
		Fee: coinAmountWithConversions(
			"0.00000004",
		),
		Invoice: "lnbc1invoice",
	}, payment)
}

func TestToLightningPaymentSend(t *testing.T) {
	lightning := makeTestLightning()

	// The wallet engine pushes signed amounts; outbound payments are negative.
	payment := lightning.toLightningPayment(walletPayment{
		ID:        "send-id",
		Kind:      paymentKindSend,
		Status:    paymentStatusPending,
		AmountSat: -100,
		FeeSat:    5,
	})

	require.Nil(t, payment.Time)
	require.Equal(t, accounts.TxTypeSend, payment.Type)
	require.Equal(t, accounts.TxStatusPending, payment.Status)
	require.Equal(t, "0.00000100", payment.Amount.Amount)
	require.Equal(t, "0.00000105", payment.DeductedAmountAtTime.Amount)
	require.Equal(t, "0.00000005", payment.Fee.Amount)
	require.True(t, payment.AmountAtTime.Estimated)
	require.True(t, payment.DeductedAmountAtTime.Estimated)
}

func TestToLightningPaymentExit(t *testing.T) {
	lightning := makeTestLightning()

	payment := lightning.toLightningPayment(walletPayment{
		ID:        "exit-id",
		Kind:      paymentKindExit,
		Status:    paymentStatusComplete,
		AmountSat: -100,
		FeeSat:    5,
		CreatedAt: 42,
	})

	require.Equal(t, accounts.TxTypeSend, payment.Type)
	require.Equal(t, accounts.TxStatusComplete, payment.Status)
	require.Equal(t, "0.00000105", payment.DeductedAmountAtTime.Amount)
	require.Nil(t, payment.BitcoinDeposit)
}

func TestToLightningPaymentDeposit(t *testing.T) {
	lightning := makeTestLightning()

	testCases := []struct {
		name          string
		status        string
		expectedState bitcoinDepositState
	}{
		{
			name:          "confirming deposit",
			status:        paymentStatusPending,
			expectedState: bitcoinDepositStateConfirming,
		},
		{
			name:          "complete deposit",
			status:        paymentStatusComplete,
			expectedState: bitcoinDepositStateComplete,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			payment := lightning.toLightningPayment(walletPayment{
				ID:        "deposit-id",
				Kind:      paymentKindDeposit,
				Status:    testCase.status,
				AmountSat: 123,
				CreatedAt: 42,
				Txid:      "deposit-txid",
			})

			require.Equal(t, accounts.TxTypeReceive, payment.Type)
			require.Equal(t, &bitcoinDeposit{
				TxID:  "deposit-txid",
				Vout:  0,
				State: testCase.expectedState,
			}, payment.BitcoinDeposit)
		})
	}
}

func TestToLightningPaymentFailed(t *testing.T) {
	lightning := makeTestLightning()

	payment := lightning.toLightningPayment(walletPayment{
		ID:            "failed-id",
		Kind:          paymentKindSend,
		Status:        paymentStatusFailed,
		AmountSat:     -100,
		FeeSat:        0,
		CreatedAt:     42,
		FailureReason: "no route",
	})

	require.Equal(t, accounts.TxStatusFailed, payment.Status)
}

func TestListPayments(t *testing.T) {
	lightning := makeActiveLightning(t)

	// No wallet state pushed yet.
	payments, err := lightning.ListPayments()
	require.Nil(t, payments)
	require.Error(t, err)

	lightning.SetState(&walletState{
		Ready: true,
		Payments: []walletPayment{
			{
				ID:        "deposit-id",
				Kind:      paymentKindDeposit,
				Status:    paymentStatusPending,
				AmountSat: 200,
				Txid:      "deposit-txid",
			},
			{
				ID:        "payment-id",
				Kind:      paymentKindReceive,
				Status:    paymentStatusComplete,
				AmountSat: 100,
				CreatedAt: 42,
			},
		},
	})

	payments, err = lightning.ListPayments()
	require.NoError(t, err)
	require.Len(t, payments, 2)
	require.Equal(t, "deposit-id", payments[0].ID)
	require.NotNil(t, payments[0].BitcoinDeposit)
	require.Equal(t, "payment-id", payments[1].ID)
	require.Nil(t, payments[1].BitcoinDeposit)
}

func TestBalance(t *testing.T) {
	lightning := makeActiveLightning(t)

	// No wallet state pushed yet.
	balance, err := lightning.Balance()
	require.Nil(t, balance)
	require.Error(t, err)

	// The wallet is not ready yet.
	lightning.SetState(&walletState{BalanceSat: 100})
	balance, err = lightning.Balance()
	require.Nil(t, balance)
	require.Error(t, err)

	lightning.SetState(&walletState{
		Ready:        true,
		BalanceSat:   100,
		PendingInSat: 500,
	})
	balance, err = lightning.Balance()
	require.NoError(t, err)
	require.Equal(t, coin.NewAmountFromInt64(100), balance.Available())
	require.Equal(t, coin.NewAmountFromInt64(500), balance.Incoming())
}

func TestTransactions(t *testing.T) {
	lightning := makeActiveLightning(t)

	// No wallet state pushed yet.
	txs, err := lightning.Transactions()
	require.Nil(t, txs)
	require.Error(t, err)

	lightning.SetState(&walletState{
		Ready: true,
		Payments: []walletPayment{
			{
				ID:        "receive-complete",
				Kind:      paymentKindReceive,
				Status:    paymentStatusComplete,
				AmountSat: 100,
				FeeSat:    1,
				CreatedAt: 100,
			},
			{
				ID:        "send-complete",
				Kind:      paymentKindSend,
				Status:    paymentStatusComplete,
				AmountSat: -30,
				FeeSat:    2,
				CreatedAt: 200,
			},
			{
				ID:        "receive-pending",
				Kind:      paymentKindReceive,
				Status:    paymentStatusPending,
				AmountSat: 1000,
				CreatedAt: 300,
			},
			{
				ID:        "send-failed",
				Kind:      paymentKindSend,
				Status:    paymentStatusFailed,
				AmountSat: -1000,
				FeeSat:    10,
				CreatedAt: 400,
			},
			{
				ID:        "receive-no-timestamp",
				Kind:      paymentKindReceive,
				Status:    paymentStatusComplete,
				AmountSat: 1000,
				CreatedAt: 0,
			},
		},
	})

	txs, err = lightning.Transactions()
	require.NoError(t, err)
	require.Len(t, txs, 3)

	timeseries, err := txs.Timeseries(time.Unix(0, 0), time.Unix(300, 0), time.Hour)
	require.Nil(t, timeseries)
	require.Equal(t, accountErrors.ErrNotAvailable, errp.Cause(err))

	hasUntimestampedReceive := false
	for _, tx := range txs {
		if tx.Timestamp == nil && tx.Type == accounts.TxTypeReceive && tx.Amount.BigInt().Cmp(big.NewInt(1000)) == 0 {
			hasUntimestampedReceive = true
		}
		if tx.Type == accounts.TxTypeReceive {
			// Fees are not deducted from receives.
			require.Nil(t, tx.Fee)
		}
	}
	require.True(t, hasUntimestampedReceive)
}

func coinAmountWithConversions(amount string) coin.FormattedAmountWithConversions {
	return coin.FormattedAmountWithConversions{
		Amount:      amount,
		Unit:        "BTC",
		Conversions: coin.ConversionsMap{},
		Estimated:   false,
	}
}

func stringPointer(value string) *string {
	return &value
}
