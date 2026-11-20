// SPDX-License-Identifier: Apache-2.0

package btc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/maketx"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	utilcfg "github.com/BitBoxSwiss/bitbox-wallet-app/util/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
)

// Spending from a multisig account takes the signatures of several keystores, which are connected
// one after the other. The first keystore signs the transaction the user proposed. The transaction
// is then kept as the pending transaction of the account until enough keystores have signed it, at
// which point it is finalized and broadcast. An account has at most one pending transaction.

const (
	// ErrMultisigTxPending is returned when proposing a transaction while the account has a pending
	// transaction.
	ErrMultisigTxPending errp.ErrorCode = "multisigTxPending"
	// ErrMultisigNoPendingTx is returned if the account has no pending transaction.
	ErrMultisigNoPendingTx errp.ErrorCode = "multisigNoPendingTx"
	// ErrMultisigAlreadySigned is returned if the connected keystore has already signed the pending
	// transaction.
	ErrMultisigAlreadySigned errp.ErrorCode = "multisigAlreadySigned"
)

// pendingMultisigTx is the persisted pending transaction of a multisig account.
type pendingMultisigTx struct {
	// PSBT is the base64 encoded PSBT with the signatures collected so far.
	PSBT      string    `json:"psbt"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// MultisigTxRecipient is an output of a pending transaction that does not pay back to the account
// as change.
type MultisigTxRecipient struct {
	Address string
	Amount  coin.Amount
}

// PendingMultisigTxInfo describes the pending transaction of a multisig account.
type PendingMultisigTxInfo struct {
	Recipients []MultisigTxRecipient
	// Amount is the sum paid to the recipients.
	Amount coin.Amount
	Fee    coin.Amount
	// Total is Amount plus Fee.
	Total     coin.Amount
	Note      string
	CreatedAt time.Time
	// Threshold is the number of signatures required to broadcast the transaction.
	Threshold int
	// SignedBy holds the root fingerprints of the keystores that have signed the transaction.
	SignedBy [][]byte
}

func (account *Account) multisig() *signing.BitcoinMultisig {
	return account.Config().SigningConfigurations.Multisig()
}

// pendingMultisigTxFilename returns the file holding the pending transaction. It is stored with
// the notes of the account, as it is user data that is not recoverable from the blockchain.
func (account *Account) pendingMultisigTxFilename() string {
	return path.Join(
		account.Config().NotesFolder,
		fmt.Sprintf("account-%s-pending-tx.json", account.Config().Code),
	)
}

func decodePSBT(encoded string) (*psbt.Packet, error) {
	packet, err := psbt.NewFromRawBytes(strings.NewReader(encoded), true)
	if err != nil {
		return nil, errp.WithStack(err)
	}
	return packet, nil
}

// loadPendingMultisigTx returns nil if there is no pending transaction.
func (account *Account) loadPendingMultisigTx() (*pendingMultisigTx, error) {
	data, err := os.ReadFile(account.pendingMultisigTxFilename())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errp.WithStack(err)
	}
	var pending pendingMultisigTx
	if err := json.Unmarshal(data, &pending); err != nil {
		return nil, errp.WithStack(err)
	}
	return &pending, nil
}

func (account *Account) storePendingMultisigTx(
	packet *psbt.Packet, note string, createdAt time.Time) error {
	encoded, err := packet.B64Encode()
	if err != nil {
		return errp.WithStack(err)
	}
	data, err := json.Marshal(pendingMultisigTx{PSBT: encoded, Note: note, CreatedAt: createdAt})
	if err != nil {
		return errp.WithStack(err)
	}
	filename := account.pendingMultisigTxFilename()
	if err := os.MkdirAll(path.Dir(filename), 0700); err != nil {
		return errp.WithStack(err)
	}
	// The transaction reveals addresses and keys of the account.
	if err := os.WriteFile(filename, data, utilcfg.PrivateFileMode); err != nil {
		return errp.WithStack(err)
	}
	return utilcfg.EnsurePrivateFile(filename)
}

func (account *Account) removePendingMultisigTx() error {
	err := os.Remove(account.pendingMultisigTxFilename())
	if err != nil && !os.IsNotExist(err) {
		return errp.WithStack(err)
	}
	return nil
}

// multisigSigners returns the root fingerprints of the keystores that have signed every input of
// the transaction.
func multisigSigners(packet *psbt.Packet) [][]byte {
	var signers [][]byte
	for inputIndex := range packet.Inputs {
		input := &packet.Inputs[inputIndex]
		var inputSigners [][]byte
		for _, partialSig := range input.PartialSigs {
			for _, derivation := range input.Bip32Derivation {
				if bytes.Equal(derivation.PubKey, partialSig.PubKey) {
					rootFingerprint := make([]byte, 4)
					binary.LittleEndian.PutUint32(rootFingerprint, derivation.MasterKeyFingerprint)
					inputSigners = append(inputSigners, rootFingerprint)
					break
				}
			}
		}
		if inputIndex == 0 {
			signers = inputSigners
			continue
		}
		var signedAllInputs [][]byte
		for _, signer := range signers {
			for _, inputSigner := range inputSigners {
				if bytes.Equal(signer, inputSigner) {
					signedAllInputs = append(signedAllInputs, signer)
					break
				}
			}
		}
		signers = signedAllInputs
	}
	return signers
}

// multisigAddress returns the address of this account with the given ID, or nil. Only outputs
// paying back to the multisig account get key info when it is spent from.
func (account *Account) multisigAddress(
	_ coin.Code, addressID addresses.AddressID) (*addresses.AccountAddress, error) {
	return account.AddressByID(addressID), nil
}

// signMultisigPSBT adds the signature of a keystore of the account to the PSBT. The keystore is
// connected first if needed. It must have the account registered, and can sign only once.
func (account *Account) signMultisigPSBT(packet *psbt.Packet) error {
	multisig := account.multisig()
	keystore, err := account.Config().ConnectKeystore()
	if err != nil {
		return err
	}
	rootFingerprint, err := keystore.RootFingerprint()
	if err != nil {
		return err
	}
	for _, signer := range multisigSigners(packet) {
		if bytes.Equal(signer, rootFingerprint) {
			return errp.WithStack(ErrMultisigAlreadySigned)
		}
	}
	// A keystore only signs for multisig accounts registered on it.
	registered, err := keystore.BTCMultisigRegistered(account.coin, multisig)
	if err != nil {
		return err
	}
	if !registered {
		return errp.WithStack(ErrMultisigNotRegistered)
	}
	return keystore.SignTransaction(&ProposedTransaction{
		TXProposal:                   &maketx.TxProposal{Coin: account.coin, Psbt: packet},
		AccountSigningConfigurations: account.Config().SigningConfigurations,
		GetPrevTx:                    account.coin.Blockchain().TransactionGet,
		FormatUnit:                   account.coin.formatUnit,
		GetKeystoreAddress:           account.multisigAddress,
	})
}

// finalizeMultisigTx assembles the signed transaction from a PSBT that has enough signatures, and
// checks that it is valid.
func finalizeMultisigTx(packet *psbt.Packet) (*wire.MsgTx, error) {
	previousOutputs := maketx.PreviousOutputs{}
	for index, txIn := range packet.UnsignedTx.TxIn {
		utxo := packet.Inputs[index].WitnessUtxo
		if utxo == nil {
			return nil, errp.Newf("input %d: the output being spent is missing", index)
		}
		previousOutputs[txIn.PreviousOutPoint] = maketx.UTXO{TxOut: utxo}
	}
	if err := psbt.MaybeFinalizeAll(packet); err != nil {
		return nil, errp.WithStack(err)
	}
	signedTx, err := psbt.Extract(packet)
	if err != nil {
		return nil, errp.WithStack(err)
	}
	sigHashes := txscript.NewTxSigHashes(signedTx, previousOutputs)
	if err := txValidityCheck(signedTx, previousOutputs, sigHashes); err != nil {
		return nil, err
	}
	return signedTx, nil
}

// CreatePendingMultisigTx signs the active transaction proposal, see TxProposal(), with a keystore
// of the account and keeps it as the pending transaction of the account.
func (account *Account) CreatePendingMultisigTx(txNote string) error {
	if account.multisig() == nil {
		return errp.New("not a multisig account")
	}
	// The proposal is read before taking multisigTxLock: TxProposal() takes the two locks in the
	// opposite order.
	unlock := account.activeTxProposalLock.RLock()
	txProposal := account.activeTxProposal
	unlock()
	if txProposal == nil {
		return errp.New("No active tx proposal")
	}
	defer account.multisigTxLock.Lock()()

	pending, err := account.loadPendingMultisigTx()
	if err != nil {
		return err
	}
	if pending != nil {
		return errp.WithStack(ErrMultisigTxPending)
	}

	proposedTransaction := &ProposedTransaction{
		TXProposal:                   txProposal,
		AccountSigningConfigurations: account.Config().SigningConfigurations,
		GetKeystoreAddress:           account.multisigAddress,
	}
	if err := proposedTransaction.Update(); err != nil {
		return err
	}
	account.log.Info("Signing multisig transaction with the first keystore")
	if err := account.signMultisigPSBT(txProposal.Psbt); err != nil {
		return errp.WithMessage(err, "Failed to sign transaction")
	}
	return account.storePendingMultisigTx(txProposal.Psbt, txNote, time.Now())
}

// SignPendingMultisigTx adds the signature of a keystore of the account to the pending
// transaction. Once enough keystores have signed, the transaction is broadcast and its ID is
// returned. The ID is empty if more signatures are needed.
//
// If broadcasting fails, the fully signed transaction stays pending and calling this function
// again broadcasts it without asking for another signature.
func (account *Account) SignPendingMultisigTx() (string, error) {
	multisig := account.multisig()
	if multisig == nil {
		return "", errp.New("not a multisig account")
	}
	defer account.multisigTxLock.Lock()()

	pending, err := account.loadPendingMultisigTx()
	if err != nil {
		return "", err
	}
	if pending == nil {
		return "", errp.WithStack(ErrMultisigNoPendingTx)
	}
	packet, err := decodePSBT(pending.PSBT)
	if err != nil {
		return "", err
	}

	if len(multisigSigners(packet)) < multisig.Threshold {
		account.log.Info("Signing pending multisig transaction")
		if err := account.signMultisigPSBT(packet); err != nil {
			return "", errp.WithMessage(err, "Failed to sign transaction")
		}
		if err := account.storePendingMultisigTx(packet, pending.Note, pending.CreatedAt); err != nil {
			return "", err
		}
		if len(multisigSigners(packet)) < multisig.Threshold {
			return "", nil
		}
	}

	signedTx, err := finalizeMultisigTx(packet)
	if err != nil {
		return "", err
	}
	account.log.Info("Signed multisig transaction is broadcasted")
	if err := account.coin.Blockchain().TransactionBroadcast(signedTx); err != nil {
		return "", err
	}
	if err := account.SetTxNote(signedTx.TxHash().String(), pending.Note); err != nil {
		// Not critical.
		account.log.WithError(err).Error("Failed to save transaction note when sending a tx")
	}
	if err := account.removePendingMultisigTx(); err != nil {
		// Not critical: the transaction is broadcast, the leftover can be discarded.
		account.log.WithError(err).Error("Failed to remove the pending transaction")
	}
	return signedTx.TxID(), nil
}

// PendingMultisigTx returns the pending transaction of the account, or nil if there is none.
func (account *Account) PendingMultisigTx() (*PendingMultisigTxInfo, error) {
	multisig := account.multisig()
	if multisig == nil {
		return nil, errp.New("not a multisig account")
	}
	defer account.multisigTxLock.Lock()()

	pending, err := account.loadPendingMultisigTx()
	if err != nil || pending == nil {
		return nil, err
	}
	packet, err := decodePSBT(pending.PSBT)
	if err != nil {
		return nil, err
	}

	var inputsSum, outputsSum, recipientsSum int64
	for index := range packet.UnsignedTx.TxIn {
		utxo := packet.Inputs[index].WitnessUtxo
		if utxo == nil {
			return nil, errp.Newf("input %d: the output being spent is missing", index)
		}
		inputsSum += utxo.Value
	}
	var recipients []MultisigTxRecipient
	for _, txOut := range packet.UnsignedTx.TxOut {
		outputsSum += txOut.Value
		if account.IsChange(addresses.NewAddressID(txOut.PkScript)) {
			continue
		}
		address := ""
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, account.coin.Net())
		if err == nil && len(addrs) == 1 {
			address = addrs[0].EncodeAddress()
		}
		recipientsSum += txOut.Value
		recipients = append(recipients, MultisigTxRecipient{
			Address: address,
			Amount:  coin.NewAmountFromInt64(txOut.Value),
		})
	}
	fee := inputsSum - outputsSum
	return &PendingMultisigTxInfo{
		Recipients: recipients,
		Amount:     coin.NewAmountFromInt64(recipientsSum),
		Fee:        coin.NewAmountFromInt64(fee),
		Total:      coin.NewAmountFromInt64(recipientsSum + fee),
		Note:       pending.Note,
		CreatedAt:  pending.CreatedAt,
		Threshold:  multisig.Threshold,
		SignedBy:   multisigSigners(packet),
	}, nil
}

// DiscardPendingMultisigTx removes the pending transaction of the account, if there is one.
func (account *Account) DiscardPendingMultisigTx() error {
	if account.multisig() == nil {
		return errp.New("not a multisig account")
	}
	defer account.multisigTxLock.Lock()()
	return account.removePendingMultisigTx()
}
