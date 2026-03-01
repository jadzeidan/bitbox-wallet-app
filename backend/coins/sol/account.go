// SPDX-License-Identifier: Apache-2.0

package sol

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"os"
	"path"
	"strconv"
	"sync/atomic"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/errors"
	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable/action"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/sirupsen/logrus"
)

const defaultFeeLamports uint64 = 5000

// TxProposal contains all data needed to sign and send a Solana transaction.
type TxProposal struct {
	Keypath   signing.AbsoluteKeypath
	Message   []byte
	Fee       uint64
	Value     uint64
	Signature []byte
}

// Account is a Solana account.
type Account struct {
	*accounts.BaseAccount

	coin *Coin
	log  *logrus.Entry

	notifier             accounts.Notifier
	signingConfiguration *signing.Configuration
	address              Address

	initialized atomic.Bool
	closed      atomic.Bool
	fatalError  atomic.Bool

	balance          coinpkg.Amount
	activeTxProposal *TxProposal
}

func NewAccount(config *accounts.AccountConfig, accountCoin *Coin, log *logrus.Entry) *Account {
	log = log.WithField("group", "sol").WithFields(logrus.Fields{"coin": accountCoin.Code(), "code": config.Config.Code})
	return &Account{
		BaseAccount: accounts.NewBaseAccount(config, accountCoin, log),
		coin:        accountCoin,
		log:         log,
		balance:     coinpkg.NewAmountFromInt64(0),
	}
}

// Info implements accounts.Interface.
func (account *Account) Info() *accounts.Info {
	if account.signingConfiguration == nil {
		return nil
	}
	return &accounts.Info{SigningConfigurations: []*signing.Configuration{account.signingConfiguration}}
}

// Initialize implements accounts.Interface.
func (account *Account) Initialize() error {
	if account.closed.Load() {
		return errp.New("Initialize: account was closed")
	}
	if account.initialized.Load() {
		return nil
	}

	signingConfigurations := account.Config().Config.SigningConfigurations
	if len(signingConfigurations) != 1 {
		return errp.New("Solana supports exactly one signing config")
	}
	cfg := signingConfigurations[0]
	if cfg.SolanaSimple == nil {
		return errp.New("Solana account requires a solana signing config")
	}
	account.signingConfiguration = cfg
	account.notifier = account.Config().GetNotifier(signingConfigurations)
	account.address = Address{address: cfg.SolanaSimple.KeyInfo.PublicKey, keypath: cfg.AbsoluteKeypath()}

	accountIdentifier := fmt.Sprintf("account-%s", account.Config().Config.Code)
	dbSubfolder := path.Join(account.Config().DBFolder, accountIdentifier)
	if err := os.MkdirAll(dbSubfolder, 0700); err != nil {
		return errp.WithStack(err)
	}

	account.coin.Initialize()
	account.initialized.Store(true)

	if err := account.refreshBalance(); err != nil {
		account.SetOffline(err)
	} else {
		account.SetOffline(nil)
	}
	// Mark initial sync done.
	done := account.Synchronizer.IncRequestsCounter()
	done()

	return account.BaseAccount.Initialize(accountIdentifier)
}

func (account *Account) refreshBalance() error {
	balance, err := account.coin.Client().GetBalance(context.TODO(), account.address.address)
	if err != nil {
		return err
	}
	account.balance = coinpkg.NewAmount(new(big.Int).SetUint64(balance))
	return nil
}

// FatalError implements accounts.Interface.
func (account *Account) FatalError() bool {
	return account.fatalError.Load()
}

// Close implements accounts.Interface.
func (account *Account) Close() {
	if account.closed.Swap(true) {
		return
	}
	account.BaseAccount.Close()
	account.Notify(observable.Event{
		Subject: string(accountsTypes.EventStatusChanged),
		Action:  action.Reload,
		Object:  nil,
	})
}

// Notifier implements accounts.Interface.
func (account *Account) Notifier() accounts.Notifier {
	return account.notifier
}

// Transactions implements accounts.Interface.
func (account *Account) Transactions() (accounts.OrderedTransactions, error) {
	if err := account.Offline(); err != nil {
		return nil, err
	}
	if !account.Synced() {
		return nil, accounts.ErrSyncInProgress
	}
	return accounts.NewOrderedTransactions(nil), nil
}

// Balance implements accounts.Interface.
func (account *Account) Balance() (*accounts.Balance, error) {
	if err := account.Offline(); err != nil {
		return nil, err
	}
	if !account.Synced() {
		return nil, accounts.ErrSyncInProgress
	}
	if err := account.refreshBalance(); err != nil {
		account.SetOffline(err)
		return nil, err
	}
	account.SetOffline(nil)
	return accounts.NewBalance(account.balance, coinpkg.NewAmountFromInt64(0)), nil
}

func decodePubkey(address string) ([32]byte, error) {
	var pubkey [32]byte
	decoded := base58.Decode(address)
	if len(decoded) != 32 {
		return pubkey, errors.ErrInvalidAddress
	}
	copy(pubkey[:], decoded)
	return pubkey, nil
}

func encodeShortVec(n uint64) []byte {
	var out []byte
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			out = append(out, b)
			return out
		}
		out = append(out, b|0x80)
	}
}

func buildTransferInstructionData(lamports uint64) []byte {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:4], 2) // SystemProgram::Transfer
	binary.LittleEndian.PutUint64(data[4:12], lamports)
	return data
}

func compileMessage(sender [32]byte, recipient [32]byte, recentBlockhash [32]byte, lamports uint64) []byte {
	accountKeys := [][32]byte{
		sender,
		recipient,
		{}, // System Program: 11111111111111111111111111111111
	}
	instructionData := buildTransferInstructionData(lamports)

	msg := []byte{1, 0, 1} // header
	msg = append(msg, encodeShortVec(uint64(len(accountKeys)))...)
	for _, key := range accountKeys {
		msg = append(msg, key[:]...)
	}
	msg = append(msg, recentBlockhash[:]...)
	msg = append(msg, encodeShortVec(1)...) // one instruction
	msg = append(msg, byte(2))              // program id index (system program)
	msg = append(msg, encodeShortVec(2)...) // two account indices
	msg = append(msg, byte(0), byte(1))
	msg = append(msg, encodeShortVec(uint64(len(instructionData)))...)
	msg = append(msg, instructionData...)
	return msg
}

func serializeSignedTx(message []byte, signature []byte) ([]byte, error) {
	if len(signature) != 64 {
		return nil, errp.New("invalid solana signature length")
	}
	encoded := append([]byte{}, encodeShortVec(1)...)
	encoded = append(encoded, signature...)
	encoded = append(encoded, message...)
	return encoded, nil
}

func (account *Account) newTx(args *accounts.TxProposalArgs) (*TxProposal, error) {
	recipient, err := decodePubkey(args.RecipientAddress)
	if err != nil {
		return nil, err
	}
	sender, err := decodePubkey(account.address.address)
	if err != nil {
		return nil, errors.ErrInvalidAddress
	}
	balance := account.balance.BigInt().Uint64()

	blockhashStr, err := account.coin.Client().GetLatestBlockhash(context.TODO())
	if err != nil {
		return nil, errors.ErrFeesNotAvailable
	}
	blockhashDecoded := base58.Decode(blockhashStr)
	if len(blockhashDecoded) != 32 {
		return nil, errors.ErrFeesNotAvailable
	}
	var recentBlockhash [32]byte
	copy(recentBlockhash[:], blockhashDecoded)

	// Compute fee from fully compiled message. Fallback to a conservative default.
	dryMessage := compileMessage(sender, recipient, recentBlockhash, 0)
	feeLamports, err := account.coin.Client().GetFeeForMessage(context.TODO(), dryMessage)
	if err != nil || feeLamports == 0 {
		feeLamports = defaultFeeLamports
	}

	amountLamports := uint64(0)
	if args.Amount.SendAll() {
		if balance <= feeLamports {
			return nil, errors.ErrInsufficientFunds
		}
		amountLamports = balance - feeLamports
	} else {
		amount, err := args.Amount.Amount(account.coin.unitFactor(), false)
		if err != nil {
			return nil, err
		}
		amountU64, err := strconv.ParseUint(amount.BigInt().String(), 10, 64)
		if err != nil || amountU64 == 0 {
			return nil, errors.ErrInvalidAmount
		}
		amountLamports = amountU64
	}

	if amountLamports+feeLamports > balance {
		return nil, errors.ErrInsufficientFunds
	}

	message := compileMessage(sender, recipient, recentBlockhash, amountLamports)
	return &TxProposal{
		Keypath: account.signingConfiguration.AbsoluteKeypath(),
		Message: message,
		Fee:     feeLamports,
		Value:   amountLamports,
	}, nil
}

// TxProposal implements accounts.Interface.
func (account *Account) TxProposal(args *accounts.TxProposalArgs) (coinpkg.Amount, coinpkg.Amount, coinpkg.Amount, error) {
	if !account.Synced() {
		return coinpkg.Amount{}, coinpkg.Amount{}, coinpkg.Amount{}, errors.ErrAccountNotsynced
	}
	if err := account.refreshBalance(); err != nil {
		return coinpkg.Amount{}, coinpkg.Amount{}, coinpkg.Amount{}, errors.ErrFeesNotAvailable
	}

	txProposal, err := account.newTx(args)
	if err != nil {
		return coinpkg.Amount{}, coinpkg.Amount{}, coinpkg.Amount{}, err
	}
	account.activeTxProposal = txProposal

	amount := coinpkg.NewAmount(new(big.Int).SetUint64(txProposal.Value))
	fee := coinpkg.NewAmount(new(big.Int).SetUint64(txProposal.Fee))
	return amount, fee, coinpkg.SumAmounts(amount, fee), nil
}

// SendTx implements accounts.Interface.
func (account *Account) SendTx(string) (string, error) {
	txProposal := account.activeTxProposal
	if txProposal == nil {
		return "", errp.New("No active tx proposal")
	}
	ks, err := account.Config().ConnectKeystore()
	if err != nil {
		return "", err
	}
	if err := ks.SignTransaction(txProposal); err != nil {
		return "", err
	}
	rawTx, err := serializeSignedTx(txProposal.Message, txProposal.Signature)
	if err != nil {
		return "", err
	}
	txID, err := account.coin.Client().SendTransaction(context.TODO(), rawTx)
	if err != nil {
		return "", err
	}
	account.Notify(observable.Event{Subject: string(accountsTypes.EventStatusChanged), Action: action.Reload, Object: nil})
	account.activeTxProposal = nil
	return txID, nil
}

// FeeTarget implements accounts.FeeTarget.
type FeeTarget struct {
	code accounts.FeeTargetCode
	fee  uint64
}

func (ft FeeTarget) Code() accounts.FeeTargetCode { return ft.code }

func (ft FeeTarget) FormattedFeeRate() string {
	return fmt.Sprintf("%d lamports", ft.fee)
}

// FeeTargets implements accounts.Interface.
func (account *Account) FeeTargets() ([]accounts.FeeTarget, accounts.FeeTargetCode) {
	feeTargets := []accounts.FeeTarget{
		FeeTarget{code: accounts.FeeTargetCodeNormal, fee: defaultFeeLamports},
	}
	return feeTargets, accounts.FeeTargetCodeNormal
}

// GetUnusedReceiveAddresses implements accounts.Interface.
func (account *Account) GetUnusedReceiveAddresses() ([]accounts.AddressList, error) {
	if !account.Synced() {
		return nil, accounts.ErrSyncInProgress
	}
	return []accounts.AddressList{{
		ScriptType: nil,
		Addresses:  []accounts.Address{account.address},
	}}, nil
}

// CanVerifyAddresses implements accounts.Interface.
func (account *Account) CanVerifyAddresses() (bool, bool, error) {
	keystore, err := account.Config().ConnectKeystore()
	if err != nil {
		return false, false, err
	}
	return keystore.CanVerifyAddress(account.Coin())
}

// VerifyAddress implements accounts.Interface.
func (account *Account) VerifyAddress(addressID string) (bool, error) {
	if !account.initialized.Load() {
		return false, errp.New("account must be initialized")
	}

	keystore, err := account.Config().ConnectKeystore()
	if err != nil {
		return false, err
	}
	canVerifyAddress, _, err := keystore.CanVerifyAddress(account.Coin())
	if err != nil {
		return false, err
	}
	if !canVerifyAddress {
		return false, nil
	}
	if addressID != account.address.ID() {
		return false, errp.New("unknown address not found")
	}
	address, err := keystore.SOLAddress(account.signingConfiguration.AbsoluteKeypath(), true)
	if err != nil {
		return false, err
	}
	if address == "" {
		// User aborted on the hardware wallet. Keep behavior aligned with BTC/ETH verification.
		return true, nil
	}
	if address != account.address.EncodeForHumans() {
		return false, errp.New("unexpected address returned by keystore")
	}
	return true, nil
}
