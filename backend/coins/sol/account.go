// SPDX-License-Identifier: Apache-2.0

package sol

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

const systemProgramAddress = "11111111111111111111111111111111"
const tokenProgramAddress = "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"
const associatedTokenProgramAddress = "ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL"
const rentSysvarAddress = "SysvarRent111111111111111111111111111111111"
const programDerivedAddressMarker = "ProgramDerivedAddress"

var fieldPrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
var sqrtM1 = mustBigInt("19681161376707505956807079304988542015446066515923890162744021073123829784752")
var edwardsD = mustBigInt("37095705934669439343138083508754565189542113879843219016388785533085940283555")

type instructionAccount struct {
	pubkey     [32]byte
	isSigner   bool
	isWritable bool
}

type compiledInstruction struct {
	programID [32]byte
	accounts  []instructionAccount
	data      []byte
}

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

	txCacheMu sync.Mutex
	txCache   accounts.OrderedTransactions
	txCacheAt time.Time
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
	if token := account.coin.Token(); token != nil {
		tokenAccounts, err := account.coin.Client().GetTokenAccountsByOwner(
			context.TODO(),
			account.address.address,
			token.Mint(),
		)
		if err != nil {
			return err
		}
		total := big.NewInt(0)
		for _, tokenAccount := range tokenAccounts {
			total.Add(total, tokenAccount.AmountBigInt())
		}
		account.balance = coinpkg.NewAmount(total)
		return nil
	}
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
	// Keep a short cache to avoid excessive RPC calls when the UI refreshes multiple endpoints.
	const txCacheTTL = 10 * time.Second
	account.txCacheMu.Lock()
	if time.Since(account.txCacheAt) < txCacheTTL && account.txCache != nil {
		cached := account.txCache
		account.txCacheMu.Unlock()
		return cached, nil
	}
	account.txCacheMu.Unlock()

	const limit = 50
	signatureAddresses, err := account.signatureLookupAddresses()
	if err != nil {
		account.SetOffline(err)
		return nil, err
	}
	signatures, err := account.fetchSignatures(signatureAddresses, limit)
	if err != nil {
		account.SetOffline(err)
		return nil, err
	}
	account.SetOffline(nil)

	txs := make([]*accounts.TransactionData, 0, len(signatures))
	for _, sig := range signatures {
		if sig.Signature == "" {
			continue
		}
		txInfo, err := account.coin.Client().GetTransaction(context.TODO(), sig.Signature)
		if err != nil {
			// Skip individual tx lookup failures to avoid dropping the whole history on partial RPC errors.
			account.log.WithError(err).Warn("getTransaction failed")
			continue
		}
		if txInfo == nil {
			continue
		}
		tx := account.txDataFromRPC(sig, txInfo)
		if tx == nil {
			continue
		}
		txs = append(txs, tx)
	}

	ordered := accounts.NewOrderedTransactions(txs)
	account.txCacheMu.Lock()
	account.txCache = ordered
	account.txCacheAt = time.Now()
	account.txCacheMu.Unlock()
	return ordered, nil
}

func (account *Account) signatureLookupAddresses() ([]string, error) {
	if token := account.coin.Token(); token != nil {
		tokenAccounts, err := account.coin.Client().GetTokenAccountsByOwner(
			context.TODO(),
			account.address.address,
			token.Mint(),
		)
		if err != nil {
			return nil, err
		}
		if len(tokenAccounts) == 0 {
			return []string{}, nil
		}
		addresses := make([]string, 0, len(tokenAccounts))
		for _, tokenAccount := range tokenAccounts {
			if tokenAccount.Pubkey == "" {
				continue
			}
			addresses = append(addresses, tokenAccount.Pubkey)
		}
		return addresses, nil
	}
	return []string{account.address.address}, nil
}

func (account *Account) fetchSignatures(addresses []string, limit int) ([]SignatureInfo, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	perAddressLimit := limit
	if perAddressLimit < 1 {
		perAddressLimit = 1
	}
	signaturesByID := make(map[string]SignatureInfo)
	for _, address := range addresses {
		signatures, err := account.coin.Client().GetSignaturesForAddress(context.TODO(), address, perAddressLimit)
		if err != nil {
			return nil, err
		}
		for _, signature := range signatures {
			if signature.Signature == "" {
				continue
			}
			existing, exists := signaturesByID[signature.Signature]
			if !exists || signature.Slot > existing.Slot {
				signaturesByID[signature.Signature] = signature
			}
		}
	}

	result := make([]SignatureInfo, 0, len(signaturesByID))
	for _, signature := range signaturesByID {
		result = append(result, signature)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Slot == result[j].Slot {
			return result[i].Signature < result[j].Signature
		}
		return result[i].Slot > result[j].Slot
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (account *Account) txDataFromRPC(sig SignatureInfo, txInfo *TransactionInfo) *accounts.TransactionData {
	if token := account.coin.Token(); token != nil {
		return account.tokenTxDataFromRPC(sig, txInfo, token)
	}
	return account.nativeTxDataFromRPC(sig, txInfo)
}

func (account *Account) nativeTxDataFromRPC(sig SignatureInfo, txInfo *TransactionInfo) *accounts.TransactionData {
	keys := txInfo.Transaction.Message.AccountKeys
	var ourIndex = -1
	for i, key := range keys {
		if key.Pubkey == account.address.address {
			ourIndex = i
			break
		}
	}
	if ourIndex < 0 {
		return nil
	}
	if ourIndex >= len(txInfo.Meta.PreBalances) || ourIndex >= len(txInfo.Meta.PostBalances) {
		return nil
	}
	pre := txInfo.Meta.PreBalances[ourIndex]
	post := txInfo.Meta.PostBalances[ourIndex]
	var delta int64 = int64(post) - int64(pre)

	var status accounts.TxStatus
	// Prefer detailed tx meta error, fallback to signature status if unavailable.
	switch {
	case txInfo.Meta.Err != nil || sig.Err != nil:
		status = accounts.TxStatusFailed
	case sig.ConfirmationStatus == "processed":
		status = accounts.TxStatusPending
	default:
		status = accounts.TxStatusComplete
	}

	var txType accounts.TxType
	feeAmount := coinpkg.NewAmount(new(big.Int).SetUint64(txInfo.Meta.Fee))
	feePtr := &feeAmount
	amountU64 := uint64(0)
	switch {
	case delta > 0:
		txType = accounts.TxTypeReceive
		feePtr = nil
		amountU64 = uint64(delta)
	case delta < 0:
		outgoing := uint64(-delta)
		if outgoing <= txInfo.Meta.Fee {
			txType = accounts.TxTypeSendSelf
			amountU64 = 0
		} else {
			txType = accounts.TxTypeSend
			amountU64 = outgoing - txInfo.Meta.Fee
		}
	default:
		// No balance change for this address; skip to reduce noisy tx entries.
		return nil
	}

	amount := coinpkg.NewAmount(new(big.Int).SetUint64(amountU64))
	tx := account.makeTransactionData(sig, txInfo, status, txType, amount, feePtr, false)

	displayAddress := account.address.address
	if txType != accounts.TxTypeReceive {
		displayAddress = account.nativeCounterpartyAddress(txInfo, txType)
		if displayAddress == "" {
			displayAddress = account.address.address
		}
	}
	ours := displayAddress == account.address.address
	tx.Addresses = []accounts.AddressAndAmount{{
		Address: displayAddress,
		Amount:  amount,
		Ours:    ours,
	}}
	return tx
}

func (account *Account) tokenTxDataFromRPC(sig SignatureInfo, txInfo *TransactionInfo, token *Token) *accounts.TransactionData {
	preAmount, postAmount := account.ownerTokenBalances(txInfo, token.Mint())
	delta := new(big.Int).Sub(postAmount, preAmount)

	var status accounts.TxStatus
	switch {
	case txInfo.Meta.Err != nil || sig.Err != nil:
		status = accounts.TxStatusFailed
	case sig.ConfirmationStatus == "processed":
		status = accounts.TxStatusPending
	default:
		status = accounts.TxStatusComplete
	}

	var txType accounts.TxType
	feeAmount := coinpkg.NewAmount(new(big.Int).SetUint64(txInfo.Meta.Fee))
	feePtr := &feeAmount
	amountBigInt := new(big.Int)
	switch delta.Sign() {
	case 1:
		txType = accounts.TxTypeReceive
		feePtr = nil
		amountBigInt = delta
	case -1:
		txType = accounts.TxTypeSend
		amountBigInt = new(big.Int).Neg(delta)
	default:
		return nil
	}

	amount := coinpkg.NewAmount(amountBigInt)
	tx := account.makeTransactionData(sig, txInfo, status, txType, amount, feePtr, true)

	displayAddress := account.address.address
	if txType == accounts.TxTypeSend {
		displayAddress = account.tokenCounterpartyAddress(txInfo, txType, token.Mint())
		if displayAddress == "" {
			displayAddress = account.address.address
		}
	}
	tx.Addresses = []accounts.AddressAndAmount{{
		Address: displayAddress,
		Amount:  amount,
		Ours:    displayAddress == account.address.address,
	}}
	return tx
}

func (account *Account) makeTransactionData(
	sig SignatureInfo,
	txInfo *TransactionInfo,
	status accounts.TxStatus,
	txType accounts.TxType,
	amount coinpkg.Amount,
	feePtr *coinpkg.Amount,
	feeIsDifferentUnit bool,
) *accounts.TransactionData {
	var timestamp *time.Time
	if txInfo.BlockTime != nil {
		t := time.Unix(*txInfo.BlockTime, 0).UTC()
		timestamp = &t
	} else if sig.BlockTime != nil {
		t := time.Unix(*sig.BlockTime, 0).UTC()
		timestamp = &t
	}

	numConfirmations := 0
	if status != accounts.TxStatusPending {
		numConfirmations = 1
	}

	txID := sig.Signature
	if txID == "" && len(txInfo.Transaction.Signatures) > 0 {
		txID = txInfo.Transaction.Signatures[0]
	}
	if txID == "" {
		return nil
	}

	return &accounts.TransactionData{
		Fee:                      feePtr,
		FeeIsDifferentUnit:       feeIsDifferentUnit,
		Timestamp:                timestamp,
		TxID:                     txID,
		InternalID:               txID,
		Height:                   int(txInfo.Slot),
		NumConfirmations:         numConfirmations,
		NumConfirmationsComplete: 1,
		Status:                   status,
		Type:                     txType,
		Amount:                   amount,
	}
}

func (account *Account) nativeCounterpartyAddress(txInfo *TransactionInfo, txType accounts.TxType) string {
	our := account.address.address
	for _, instruction := range txInfo.Transaction.Message.Instructions {
		if instruction.Parsed == nil {
			continue
		}
		if instruction.Program != "system" {
			continue
		}
		if instruction.Parsed.Type != "transfer" && instruction.Parsed.Type != "transferWithSeed" {
			continue
		}

		source, _ := infoString(instruction.Parsed.Info, "source")
		destination, _ := infoString(instruction.Parsed.Info, "destination")
		if source == "" && destination == "" {
			continue
		}
		switch txType {
		case accounts.TxTypeSend:
			if source == our && destination != "" {
				return destination
			}
		case accounts.TxTypeReceive:
			if destination == our && source != "" {
				return source
			}
		case accounts.TxTypeSendSelf:
			if source == our && destination != "" {
				return destination
			}
		}
	}

	// Fallback when parsed instructions are absent/incomplete.
	for _, key := range txInfo.Transaction.Message.AccountKeys {
		pubkey := key.Pubkey
		if pubkey == "" || pubkey == our || pubkey == systemProgramAddress {
			continue
		}
		return pubkey
	}
	return ""
}

func (account *Account) tokenCounterpartyAddress(txInfo *TransactionInfo, txType accounts.TxType, mint string) string {
	our := account.address.address
	owners := account.tokenAccountOwners(txInfo, mint)
	for _, instruction := range txInfo.Transaction.Message.Instructions {
		if instruction.Parsed == nil {
			continue
		}
		program := strings.ToLower(instruction.Program)
		if program != "spl-token" && program != "spl-token-2022" {
			continue
		}
		if instruction.Parsed.Type != "transfer" && instruction.Parsed.Type != "transferChecked" {
			continue
		}

		sourceAccount, _ := infoString(instruction.Parsed.Info, "source")
		destinationAccount, _ := infoString(instruction.Parsed.Info, "destination")
		sourceOwner := owners[sourceAccount]
		destinationOwner := owners[destinationAccount]
		switch txType {
		case accounts.TxTypeSend:
			if sourceOwner == our && destinationOwner != "" {
				return destinationOwner
			}
			if sourceOwner == our && destinationAccount != "" {
				return destinationAccount
			}
		case accounts.TxTypeReceive:
			if destinationOwner == our && sourceOwner != "" {
				return sourceOwner
			}
		}
	}
	return ""
}

func (account *Account) ownerTokenBalances(txInfo *TransactionInfo, mint string) (*big.Int, *big.Int) {
	pre := big.NewInt(0)
	post := big.NewInt(0)
	for _, balance := range txInfo.Meta.PreTokenBalances {
		if balance.Owner == account.address.address && balance.Mint == mint {
			if amount, ok := new(big.Int).SetString(balance.UITokenAmount.Amount, 10); ok {
				pre.Add(pre, amount)
			}
		}
	}
	for _, balance := range txInfo.Meta.PostTokenBalances {
		if balance.Owner == account.address.address && balance.Mint == mint {
			if amount, ok := new(big.Int).SetString(balance.UITokenAmount.Amount, 10); ok {
				post.Add(post, amount)
			}
		}
	}
	return pre, post
}

func (account *Account) tokenAccountOwners(txInfo *TransactionInfo, mint string) map[string]string {
	owners := map[string]string{}
	update := func(tokenBalances []rpcTokenBalance) {
		for _, balance := range tokenBalances {
			if balance.Mint != mint || balance.Owner == "" {
				continue
			}
			if balance.AccountIndex < 0 || balance.AccountIndex >= len(txInfo.Transaction.Message.AccountKeys) {
				continue
			}
			pubkey := txInfo.Transaction.Message.AccountKeys[balance.AccountIndex].Pubkey
			if pubkey == "" {
				continue
			}
			owners[pubkey] = balance.Owner
		}
	}
	update(txInfo.Meta.PreTokenBalances)
	update(txInfo.Meta.PostTokenBalances)
	return owners
}

func infoString(info map[string]interface{}, key string) (string, bool) {
	value, ok := info[key]
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		return typed, true
	default:
		// Defensive fallback in case the RPC gives a typed JSON value.
		bytes, err := json.Marshal(typed)
		if err != nil {
			return "", false
		}
		var result string
		if err := json.Unmarshal(bytes, &result); err != nil {
			return "", false
		}
		return result, true
	}
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

func buildSystemTransferInstructionData(lamports uint64) []byte {
	data := make([]byte, 12)
	binary.LittleEndian.PutUint32(data[0:4], 2) // SystemProgram::Transfer
	binary.LittleEndian.PutUint64(data[4:12], lamports)
	return data
}

func buildTokenTransferInstructionData(amount uint64) []byte {
	data := make([]byte, 9)
	data[0] = 3 // Transfer
	binary.LittleEndian.PutUint64(data[1:9], amount)
	return data
}

func compileMessage(instructions []compiledInstruction, recentBlockhash [32]byte) []byte {
	if len(instructions) == 0 {
		return nil
	}

	addedOrder := make([][32]byte, 0)
	accountMeta := map[[32]byte]instructionAccount{}
	addAccount := func(account instructionAccount) {
		existing, exists := accountMeta[account.pubkey]
		if !exists {
			addedOrder = append(addedOrder, account.pubkey)
			accountMeta[account.pubkey] = account
			return
		}
		existing.isSigner = existing.isSigner || account.isSigner
		existing.isWritable = existing.isWritable || account.isWritable
		accountMeta[account.pubkey] = existing
	}

	for _, instruction := range instructions {
		for _, account := range instruction.accounts {
			addAccount(account)
		}
		addAccount(instructionAccount{
			pubkey:     instruction.programID,
			isSigner:   false,
			isWritable: false,
		})
	}

	var signedWritable []instructionAccount
	var signedReadonly []instructionAccount
	var unsignedWritable []instructionAccount
	var unsignedReadonly []instructionAccount

	for _, pubkey := range addedOrder {
		account := accountMeta[pubkey]
		switch {
		case account.isSigner && account.isWritable:
			signedWritable = append(signedWritable, account)
		case account.isSigner && !account.isWritable:
			signedReadonly = append(signedReadonly, account)
		case !account.isSigner && account.isWritable:
			unsignedWritable = append(unsignedWritable, account)
		default:
			unsignedReadonly = append(unsignedReadonly, account)
		}
	}

	orderedAccounts := append([]instructionAccount{}, signedWritable...)
	orderedAccounts = append(orderedAccounts, signedReadonly...)
	orderedAccounts = append(orderedAccounts, unsignedWritable...)
	orderedAccounts = append(orderedAccounts, unsignedReadonly...)

	accountIndices := make(map[[32]byte]byte, len(orderedAccounts))
	for i, account := range orderedAccounts {
		accountIndices[account.pubkey] = byte(i)
	}

	message := []byte{
		byte(len(signedWritable) + len(signedReadonly)),
		byte(len(signedReadonly)),
		byte(len(unsignedReadonly)),
	}

	message = append(message, encodeShortVec(uint64(len(orderedAccounts)))...)
	for _, account := range orderedAccounts {
		message = append(message, account.pubkey[:]...)
	}
	message = append(message, recentBlockhash[:]...)

	message = append(message, encodeShortVec(uint64(len(instructions)))...)
	for _, instruction := range instructions {
		programIndex := accountIndices[instruction.programID]
		message = append(message, programIndex)
		message = append(message, encodeShortVec(uint64(len(instruction.accounts)))...)
		for _, account := range instruction.accounts {
			message = append(message, accountIndices[account.pubkey])
		}
		message = append(message, encodeShortVec(uint64(len(instruction.data)))...)
		message = append(message, instruction.data...)
	}
	return message
}

func mustBigInt(value string) *big.Int {
	result, ok := new(big.Int).SetString(value, 10)
	if !ok {
		panic("invalid big int constant")
	}
	return result
}

func isOnEd25519Curve(pubkey [32]byte) bool {
	yBytes := pubkey
	sign := (yBytes[31] >> 7) & 1
	yBytes[31] &= 0x7f
	reverseInPlace(yBytes[:])
	y := new(big.Int).SetBytes(yBytes[:])
	if y.Cmp(fieldPrime) >= 0 {
		return false
	}

	y2 := new(big.Int).Mul(y, y)
	y2.Mod(y2, fieldPrime)

	u := new(big.Int).Sub(y2, big.NewInt(1))
	u.Mod(u, fieldPrime)

	v := new(big.Int).Mul(edwardsD, y2)
	v.Add(v, big.NewInt(1))
	v.Mod(v, fieldPrime)
	if v.Sign() == 0 {
		return false
	}

	vInv := new(big.Int).ModInverse(v, fieldPrime)
	if vInv == nil {
		return false
	}
	x2 := new(big.Int).Mul(u, vInv)
	x2.Mod(x2, fieldPrime)

	exp := new(big.Int).Add(fieldPrime, big.NewInt(3))
	exp.Rsh(exp, 3)
	x := new(big.Int).Exp(x2, exp, fieldPrime)

	vx2 := new(big.Int).Mul(v, new(big.Int).Mul(x, x))
	vx2.Mod(vx2, fieldPrime)
	if vx2.Cmp(u) != 0 {
		negU := new(big.Int).Neg(u)
		negU.Mod(negU, fieldPrime)
		if vx2.Cmp(negU) != 0 {
			return false
		}
		x.Mul(x, sqrtM1)
		x.Mod(x, fieldPrime)
	}

	if x.Bit(0) != uint(sign) {
		x.Sub(fieldPrime, x)
	}
	if x.Sign() == 0 && sign == 1 {
		return false
	}
	return true
}

func reverseInPlace(bytes []byte) {
	for left, right := 0, len(bytes)-1; left < right; left, right = left+1, right-1 {
		bytes[left], bytes[right] = bytes[right], bytes[left]
	}
}

func findProgramAddress(seeds [][]byte, programID [32]byte) ([32]byte, error) {
	var derived [32]byte
	seedPrefix := []byte{}
	for _, seed := range seeds {
		seedPrefix = append(seedPrefix, seed...)
	}
	for bump := 255; bump >= 0; bump-- {
		input := make([]byte, 0, len(seedPrefix)+1+32+len(programDerivedAddressMarker))
		input = append(input, seedPrefix...)
		input = append(input, byte(bump))
		input = append(input, programID[:]...)
		input = append(input, []byte(programDerivedAddressMarker)...)
		hash := sha256.Sum256(input)
		if !isOnEd25519Curve(hash) {
			return hash, nil
		}
	}
	return derived, errp.New("unable to derive program address")
}

func associatedTokenAddress(owner [32]byte, mint [32]byte) ([32]byte, error) {
	associatedTokenProgram, err := decodePubkey(associatedTokenProgramAddress)
	if err != nil {
		return [32]byte{}, err
	}
	tokenProgram, err := decodePubkey(tokenProgramAddress)
	if err != nil {
		return [32]byte{}, err
	}
	seeds := [][]byte{
		owner[:],
		tokenProgram[:],
		mint[:],
	}
	return findProgramAddress(seeds, associatedTokenProgram)
}

func createAssociatedTokenAccountInstruction(
	payer [32]byte,
	associatedTokenAccount [32]byte,
	owner [32]byte,
	mint [32]byte,
) (compiledInstruction, error) {
	associatedTokenProgram, err := decodePubkey(associatedTokenProgramAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	systemProgram, err := decodePubkey(systemProgramAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	tokenProgram, err := decodePubkey(tokenProgramAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	rentProgram, err := decodePubkey(rentSysvarAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	return compiledInstruction{
		programID: associatedTokenProgram,
		accounts: []instructionAccount{
			{pubkey: payer, isSigner: true, isWritable: true},
			{pubkey: associatedTokenAccount, isSigner: false, isWritable: true},
			{pubkey: owner, isSigner: false, isWritable: false},
			{pubkey: mint, isSigner: false, isWritable: false},
			{pubkey: systemProgram, isSigner: false, isWritable: false},
			{pubkey: tokenProgram, isSigner: false, isWritable: false},
			{pubkey: rentProgram, isSigner: false, isWritable: false},
		},
		data: []byte{},
	}, nil
}

func tokenTransferInstruction(source [32]byte, destination [32]byte, authority [32]byte, amount uint64) (compiledInstruction, error) {
	tokenProgram, err := decodePubkey(tokenProgramAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	return compiledInstruction{
		programID: tokenProgram,
		accounts: []instructionAccount{
			{pubkey: source, isSigner: false, isWritable: true},
			{pubkey: destination, isSigner: false, isWritable: true},
			{pubkey: authority, isSigner: true, isWritable: true},
		},
		data: buildTokenTransferInstructionData(amount),
	}, nil
}

func systemTransferInstruction(sender [32]byte, recipient [32]byte, amount uint64) (compiledInstruction, error) {
	systemProgram, err := decodePubkey(systemProgramAddress)
	if err != nil {
		return compiledInstruction{}, err
	}
	return compiledInstruction{
		programID: systemProgram,
		accounts: []instructionAccount{
			{pubkey: sender, isSigner: true, isWritable: true},
			{pubkey: recipient, isSigner: false, isWritable: true},
		},
		data: buildSystemTransferInstructionData(amount),
	}, nil
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
	if account.coin.Token() != nil {
		return account.newTokenTx(args)
	}
	return account.newNativeTx(args)
}

func (account *Account) fetchRecentBlockhash() ([32]byte, error) {
	var recentBlockhash [32]byte
	blockhashStr, err := account.coin.Client().GetLatestBlockhash(context.TODO())
	if err != nil {
		return recentBlockhash, errors.ErrFeesNotAvailable
	}
	blockhashDecoded := base58.Decode(blockhashStr)
	if len(blockhashDecoded) != 32 {
		return recentBlockhash, errors.ErrFeesNotAvailable
	}
	copy(recentBlockhash[:], blockhashDecoded)
	return recentBlockhash, nil
}

func (account *Account) estimateFee(message []byte) uint64 {
	feeLamports, err := account.coin.Client().GetFeeForMessage(context.TODO(), message)
	if err != nil || feeLamports == 0 {
		return defaultFeeLamports
	}
	return feeLamports
}

func (account *Account) newNativeTx(args *accounts.TxProposalArgs) (*TxProposal, error) {
	recipient, err := decodePubkey(args.RecipientAddress)
	if err != nil {
		return nil, err
	}
	sender, err := decodePubkey(account.address.address)
	if err != nil {
		return nil, errors.ErrInvalidAddress
	}
	balance := account.balance.BigInt().Uint64()

	recentBlockhash, err := account.fetchRecentBlockhash()
	if err != nil {
		return nil, err
	}
	dryInstruction, err := systemTransferInstruction(sender, recipient, 0)
	if err != nil {
		return nil, err
	}
	feeLamports := account.estimateFee(compileMessage([]compiledInstruction{dryInstruction}, recentBlockhash))

	amountLamports := uint64(0)
	if args.Amount.SendAll() {
		if balance <= feeLamports {
			return nil, errors.ErrInsufficientFunds
		}
		amountLamports = balance - feeLamports
	} else {
		amount, err := args.Amount.Amount(account.coin.unitFactor(false), false)
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

	transferInstruction, err := systemTransferInstruction(sender, recipient, amountLamports)
	if err != nil {
		return nil, err
	}
	message := compileMessage([]compiledInstruction{transferInstruction}, recentBlockhash)
	return &TxProposal{
		Keypath: account.signingConfiguration.AbsoluteKeypath(),
		Message: message,
		Fee:     feeLamports,
		Value:   amountLamports,
	}, nil
}

func (account *Account) newTokenTx(args *accounts.TxProposalArgs) (*TxProposal, error) {
	token := account.coin.Token()
	if token == nil {
		return nil, errp.New("token metadata missing")
	}

	senderOwner, err := decodePubkey(account.address.address)
	if err != nil {
		return nil, errors.ErrInvalidAddress
	}
	recipientOwner, err := decodePubkey(args.RecipientAddress)
	if err != nil {
		return nil, err
	}

	sourceTokenAccounts, err := account.coin.Client().GetTokenAccountsByOwner(
		context.TODO(),
		account.address.address,
		token.Mint(),
	)
	if err != nil {
		return nil, err
	}
	if len(sourceTokenAccounts) == 0 {
		return nil, errors.ErrInsufficientFunds
	}

	sourceTokenPubkey := [32]byte{}
	sourceBalance := big.NewInt(0)
	selectSourceTokenAccount := func(amountUnits *big.Int) error {
		for _, tokenAccount := range sourceTokenAccounts {
			amount := tokenAccount.AmountBigInt()
			if amount.Sign() <= 0 {
				continue
			}
			if amountUnits != nil && amount.Cmp(amountUnits) < 0 {
				continue
			}
			tokenPubkey, err := decodePubkey(tokenAccount.Pubkey)
			if err != nil {
				continue
			}
			sourceTokenPubkey = tokenPubkey
			sourceBalance = amount
			return nil
		}
		return errors.ErrInsufficientFunds
	}

	destinationTokenAccounts, err := account.coin.Client().GetTokenAccountsByOwner(context.TODO(), args.RecipientAddress, token.Mint())
	if err != nil {
		return nil, err
	}
	destinationTokenPubkey := [32]byte{}
	createAssociatedTokenAccount := false
	if len(destinationTokenAccounts) > 0 {
		destinationTokenPubkey, err = decodePubkey(destinationTokenAccounts[0].Pubkey)
		if err != nil {
			return nil, errors.ErrInvalidAddress
		}
	} else {
		mintPubkey, err := decodePubkey(token.Mint())
		if err != nil {
			return nil, errors.ErrInvalidAddress
		}
		destinationTokenPubkey, err = associatedTokenAddress(recipientOwner, mintPubkey)
		if err != nil {
			return nil, err
		}
		createAssociatedTokenAccount = true
	}

	prepareInstructions := func(amount uint64, recentBlockhash [32]byte) ([]compiledInstruction, uint64, error) {
		instructions := make([]compiledInstruction, 0, 2)
		if createAssociatedTokenAccount {
			mintPubkey, err := decodePubkey(token.Mint())
			if err != nil {
				return nil, 0, errors.ErrInvalidAddress
			}
			createInstruction, err := createAssociatedTokenAccountInstruction(
				senderOwner,
				destinationTokenPubkey,
				recipientOwner,
				mintPubkey,
			)
			if err != nil {
				return nil, 0, err
			}
			instructions = append(instructions, createInstruction)
		}
		transferInstruction, err := tokenTransferInstruction(
			sourceTokenPubkey,
			destinationTokenPubkey,
			senderOwner,
			amount,
		)
		if err != nil {
			return nil, 0, err
		}
		instructions = append(instructions, transferInstruction)
		fee := account.estimateFee(compileMessage(instructions, recentBlockhash))
		return instructions, fee, nil
	}

	amountUnits := big.NewInt(0)
	if args.Amount.SendAll() {
		// Determine amount after we know the exact fee and source token account.
		if err := selectSourceTokenAccount(nil); err != nil {
			return nil, err
		}
		amountUnits = new(big.Int).Set(sourceBalance)
	} else {
		amount, err := args.Amount.Amount(account.coin.unitFactor(false), false)
		if err != nil {
			return nil, err
		}
		if amount.BigInt().Sign() <= 0 {
			return nil, errors.ErrInvalidAmount
		}
		amountUnits = amount.BigInt()
	}

	solBalance, err := account.coin.Client().GetBalance(context.TODO(), account.address.address)
	if err != nil {
		return nil, errors.ErrFeesNotAvailable
	}

	recentBlockhash, err := account.fetchRecentBlockhash()
	if err != nil {
		return nil, err
	}

	var instructions []compiledInstruction
	feeLamports := uint64(0)
	if args.Amount.SendAll() {
		amountU64, parseErr := strconv.ParseUint(amountUnits.String(), 10, 64)
		if parseErr != nil {
			return nil, errors.ErrInvalidAmount
		}
		instructions, feeLamports, err = prepareInstructions(amountU64, recentBlockhash)
		if err != nil {
			return nil, err
		}
		if solBalance < feeLamports {
			return nil, errors.ErrInsufficientFunds
		}
	} else {
		if err := selectSourceTokenAccount(amountUnits); err != nil {
			return nil, err
		}
		amountU64, parseErr := strconv.ParseUint(amountUnits.String(), 10, 64)
		if parseErr != nil {
			return nil, errors.ErrInvalidAmount
		}
		instructions, feeLamports, err = prepareInstructions(amountU64, recentBlockhash)
		if err != nil {
			return nil, err
		}
		if solBalance < feeLamports {
			return nil, errors.ErrInsufficientFunds
		}
	}

	if sourceBalance.Sign() <= 0 {
		return nil, errors.ErrInsufficientFunds
	}
	message := compileMessage(instructions, recentBlockhash)
	amountU64, err := strconv.ParseUint(amountUnits.String(), 10, 64)
	if err != nil {
		return nil, errors.ErrInvalidAmount
	}
	return &TxProposal{
		Keypath: account.signingConfiguration.AbsoluteKeypath(),
		Message: message,
		Fee:     feeLamports,
		Value:   amountU64,
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
	total := coinpkg.SumAmounts(amount, fee)
	if account.coin.Token() != nil {
		total = amount
	}
	return amount, fee, total, nil
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
	account.txCacheMu.Lock()
	account.txCache = nil
	account.txCacheAt = time.Time{}
	account.txCacheMu.Unlock()
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
