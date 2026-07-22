// SPDX-License-Identifier: Apache-2.0

package lightning

import (
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
)

// walletState is a snapshot of the wavelength wallet engine state pushed by the frontend.
// It must match the frontend wire type exactly.
type walletState struct {
	Ready           bool            `json:"ready"`
	ServerConnected bool            `json:"serverConnected"`
	BlockHeight     uint32          `json:"blockHeight"`
	BalanceSat      uint64          `json:"balanceSat"` // wavelength Balance.confirmedSat
	PendingInSat    uint64          `json:"pendingInSat"`
	PendingOutSat   uint64          `json:"pendingOutSat"`
	Payments        []walletPayment `json:"payments"`
}

// walletPayment is a single activity entry of the wavelength wallet engine, pushed by the
// frontend as part of walletState. It must match the frontend wire type exactly.
type walletPayment struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`      // "send" | "receive" | "deposit" | "exit"
	Status        string `json:"status"`    // "pending" | "complete" | "failed"
	AmountSat     int64  `json:"amountSat"` // signed: positive inbound, negative outbound (wavelength Entry.amountSat)
	FeeSat        uint64 `json:"feeSat"`
	CreatedAt     int64  `json:"createdAt"` // unix seconds
	Note          string `json:"note,omitempty"`
	Invoice       string `json:"invoice,omitempty"`     // Entry.request.lightningInvoice
	PaymentHash   string `json:"paymentHash,omitempty"` // Entry.progress.paymentHash
	Txid          string `json:"txid,omitempty"`        // Entry.progress.txid
	FailureReason string `json:"failureReason,omitempty"`
}

const (
	paymentKindSend    = "send"
	paymentKindReceive = "receive"
	paymentKindDeposit = "deposit"
	paymentKindExit    = "exit"
)

const (
	paymentStatusPending  = "pending"
	paymentStatusComplete = "complete"
	paymentStatusFailed   = "failed"
)

type bitcoinDepositState string

const (
	bitcoinDepositStateConfirming bitcoinDepositState = "confirming"
	bitcoinDepositStateComplete   bitcoinDepositState = "complete"
)

type bitcoinDeposit struct {
	TxID  string              `json:"txid"`
	Vout  uint32              `json:"vout"`
	State bitcoinDepositState `json:"state"`
}

type lightningPayment struct {
	ID                   string                              `json:"id"`
	Type                 accounts.TxType                     `json:"type"`
	Status               accounts.TxStatus                   `json:"status"`
	Time                 *string                             `json:"time"`
	Description          string                              `json:"description,omitempty"`
	Amount               coin.FormattedAmountWithConversions `json:"amount"`
	AmountAtTime         coin.FormattedAmountWithConversions `json:"amountAtTime"`
	DeductedAmountAtTime coin.FormattedAmountWithConversions `json:"deductedAmountAtTime"`
	Fee                  coin.FormattedAmountWithConversions `json:"fee"`
	Invoice              string                              `json:"invoice,omitempty"`
	BitcoinDeposit       *bitcoinDeposit                     `json:"bitcoinDeposit,omitempty"`
}

func toLightningPaymentType(kind string) accounts.TxType {
	switch kind {
	case paymentKindReceive, paymentKindDeposit:
		return accounts.TxTypeReceive
	default:
		return accounts.TxTypeSend
	}
}

func toLightningPaymentStatus(status string) accounts.TxStatus {
	switch status {
	case paymentStatusComplete:
		return accounts.TxStatusComplete
	case paymentStatusFailed:
		return accounts.TxStatusFailed
	default:
		return accounts.TxStatusPending
	}
}

// paymentAmountSat returns the absolute payment amount. The wallet engine pushes signed
// amounts (positive inbound, negative outbound), while the app-facing contract carries the
// direction in the payment type.
func paymentAmountSat(payment walletPayment) int64 {
	if payment.AmountSat < 0 {
		return -payment.AmountSat
	}
	return payment.AmountSat
}

func (lightning *Lightning) toLightningPayment(payment walletPayment) lightningPayment {
	paymentType := toLightningPaymentType(payment.Kind)
	status := toLightningPaymentStatus(payment.Status)
	amount := coin.NewAmountFromInt64(paymentAmountSat(payment))
	fee := coin.NewAmountFromInt64(int64(payment.FeeSat))
	deductedAmount := coin.NewAmountFromInt64(0)
	if paymentType == accounts.TxTypeSend {
		deductedAmount = coin.SumAmounts(amount, fee)
	}

	var timestamp *time.Time
	var formattedTime *string
	if payment.CreatedAt > 0 {
		t := time.Unix(payment.CreatedAt, 0).UTC()
		timestamp = &t
		formatted := t.Format(time.RFC3339)
		formattedTime = &formatted
	}

	result := lightningPayment{
		ID:                   payment.ID,
		Type:                 paymentType,
		Status:               status,
		Time:                 formattedTime,
		Description:          payment.Note,
		Amount:               amount.FormatWithConversions(lightning.btcCoin, false, lightning.ratesUpdater),
		AmountAtTime:         amount.FormatWithConversionsAtTime(lightning.btcCoin, timestamp, lightning.ratesUpdater),
		DeductedAmountAtTime: deductedAmount.FormatWithConversionsAtTime(lightning.btcCoin, timestamp, lightning.ratesUpdater),
		Fee:                  fee.FormatWithConversions(lightning.btcCoin, true, lightning.ratesUpdater),
		Invoice:              payment.Invoice,
	}
	// Mark deposits as top-ups so the frontend can identify them reliably.
	if payment.Kind == paymentKindDeposit {
		depositState := bitcoinDepositStateConfirming
		if status == accounts.TxStatusComplete {
			depositState = bitcoinDepositStateComplete
		}
		result.BitcoinDeposit = &bitcoinDeposit{
			TxID:  payment.Txid,
			Vout:  0,
			State: depositState,
		}
	}

	return result
}

func toLightningTransaction(payment walletPayment) *accounts.TransactionData {
	if toLightningPaymentStatus(payment.Status) != accounts.TxStatusComplete {
		return nil
	}

	var timestamp *time.Time
	if payment.CreatedAt != 0 {
		t := time.Unix(payment.CreatedAt, 0).UTC()
		timestamp = &t
	}
	paymentType := toLightningPaymentType(payment.Kind)
	amount := coin.NewAmountFromInt64(paymentAmountSat(payment))
	fee := coin.NewAmountFromInt64(int64(payment.FeeSat))

	tx := &accounts.TransactionData{
		Fee:              &fee,
		Timestamp:        timestamp,
		Height:           1,
		Status:           accounts.TxStatusComplete,
		Type:             paymentType,
		Amount:           amount,
		CreatedTimestamp: timestamp,
	}
	if paymentType == accounts.TxTypeReceive {
		tx.Fee = nil
	}
	return tx
}

// ListPayments converts the payments of the last pushed wallet state to the app-facing
// contract.
func (lightning *Lightning) ListPayments() ([]lightningPayment, error) {
	state, err := lightning.readyState()
	if err != nil {
		return nil, err
	}

	payments := make([]lightningPayment, 0, len(state.Payments))
	for _, payment := range state.Payments {
		payments = append(payments, lightning.toLightningPayment(payment))
	}
	return payments, nil
}

// Transactions converts the payments of the last pushed wallet state to generic transaction
// data for charting.
func (lightning *Lightning) Transactions() (accounts.OrderedTransactions, error) {
	state, err := lightning.readyState()
	if err != nil {
		return nil, err
	}

	txs := make([]*accounts.TransactionData, 0, len(state.Payments))
	for _, payment := range state.Payments {
		tx := toLightningTransaction(payment)
		if tx != nil {
			txs = append(txs, tx)
		}
	}
	return accounts.NewOrderedTransactions(txs), nil
}
