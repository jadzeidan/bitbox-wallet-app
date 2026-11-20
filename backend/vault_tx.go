// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/jsonp"
)

// Spending from a vault takes the signatures of two of its keystores, which are connected one
// after the other. The transaction is pending in between, see btc.Account.CreatePendingMultisigTx.

// vaultTxAccount is the part of a vault account that is used to spend from it.
type vaultTxAccount interface {
	CreatePendingMultisigTx(txNote string) error
	SignPendingMultisigTx() (string, error)
	PendingMultisigTx() (*btc.PendingMultisigTxInfo, error)
	DiscardPendingMultisigTx() error
}

// VaultTxKey describes a key of a vault with regard to its pending transaction.
type VaultTxKey struct {
	RootFingerprint jsonp.HexBytes `json:"rootFingerprint"`
	// KeystoreName is the name of the keystore holding the key. It is empty if the keystore has
	// never been connected.
	KeystoreName string `json:"keystoreName"`
	// Connected is true if the key is held by the connected keystore.
	Connected bool `json:"connected"`
	// Signed is true if the keystore holding the key has signed the pending transaction.
	Signed bool `json:"signed"`
}

// VaultTxRecipient is an output of the pending transaction of a vault that is not change.
type VaultTxRecipient struct {
	Address string                                 `json:"address"`
	Amount  coinpkg.FormattedAmountWithConversions `json:"amount"`
}

// VaultTxInfo describes the pending transaction of a vault.
type VaultTxInfo struct {
	Recipients []VaultTxRecipient                     `json:"recipients"`
	Amount     coinpkg.FormattedAmountWithConversions `json:"amount"`
	Fee        coinpkg.FormattedAmountWithConversions `json:"fee"`
	Total      coinpkg.FormattedAmountWithConversions `json:"total"`
	Note       string                                 `json:"note"`
	// Threshold is the number of signatures needed to send the transaction.
	Threshold int          `json:"threshold"`
	Keys      []VaultTxKey `json:"keys"`
	// NumSignatures is the number of keystores that have signed.
	NumSignatures int `json:"numSignatures"`
	// CanSign is true if the connected keystore holds a key of the vault and has not signed yet.
	CanSign bool `json:"canSign"`
	// CanBroadcast is true if the transaction has enough signatures and only needs to be sent.
	// This is the case if sending it failed before.
	CanBroadcast bool `json:"canBroadcast"`
}

// vaultTxAccount returns the loaded vault account with the given code.
func (backend *Backend) vaultTxAccount(accountCode accountsTypes.Code) (*AccountView, vaultTxAccount, error) {
	accountView := backend.Accounts().lookup(accountCode)
	if accountView == nil {
		return nil, nil, errp.Newf("Could not find account %s", accountCode)
	}
	if !accountView.Record.IsMultisig() {
		return nil, nil, errp.Newf("Account %s is not a vault", accountCode)
	}
	account, ok := accountView.Account.(vaultTxAccount)
	if !ok {
		return nil, nil, errp.Newf("Account %s can't be spent from as a vault", accountCode)
	}
	if err := accountView.Account.Initialize(); err != nil {
		return nil, nil, err
	}
	return accountView, account, nil
}

func (backend *Backend) formatVaultTxAmount(
	coin coinpkg.Coin, amount coinpkg.Amount, isFee bool) coinpkg.FormattedAmountWithConversions {
	return coinpkg.FormattedAmountWithConversions{
		Amount:      coin.FormatAmount(amount, isFee),
		Unit:        coin.GetFormatUnit(isFee),
		Conversions: coinpkg.Conversions(amount, coin, isFee, backend.ratesUpdater),
	}
}

// VaultPendingTx returns the pending transaction of a vault account, or nil if there is none.
func (backend *Backend) VaultPendingTx(accountCode accountsTypes.Code) (*VaultTxInfo, error) {
	accountView, account, err := backend.vaultTxAccount(accountCode)
	if err != nil {
		return nil, err
	}
	pending, err := account.PendingMultisigTx()
	if err != nil || pending == nil {
		return nil, err
	}
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return nil, err
	}
	var connectedRootFingerprint []byte
	if connectedKeystore := backend.Keystore(); connectedKeystore != nil {
		connectedRootFingerprint, err = connectedKeystore.RootFingerprint()
		if err != nil {
			return nil, err
		}
	}

	coin := accountView.Account.Coin()
	info := &VaultTxInfo{
		Amount:        backend.formatVaultTxAmount(coin, pending.Amount, false),
		Fee:           backend.formatVaultTxAmount(coin, pending.Fee, true),
		Total:         backend.formatVaultTxAmount(coin, pending.Total, false),
		Note:          pending.Note,
		Threshold:     pending.Threshold,
		NumSignatures: len(pending.SignedBy),
		CanBroadcast:  len(pending.SignedBy) >= pending.Threshold,
	}
	for _, recipient := range pending.Recipients {
		info.Recipients = append(info.Recipients, VaultTxRecipient{
			Address: recipient.Address,
			Amount:  backend.formatVaultTxAmount(coin, recipient.Amount, false),
		})
	}
	for _, rootFingerprint := range accountView.Record.SigningConfigurations.RootFingerprints() {
		key := VaultTxKey{
			RootFingerprint: rootFingerprint,
			Connected: connectedRootFingerprint != nil &&
				bytes.Equal(rootFingerprint, connectedRootFingerprint),
		}
		if persistedKeystore, err := accountsConfig.LookupKeystore(rootFingerprint); err == nil {
			key.KeystoreName = persistedKeystore.Name
		}
		for _, signer := range pending.SignedBy {
			if bytes.Equal(signer, rootFingerprint) {
				key.Signed = true
			}
		}
		if key.Connected && !key.Signed && !info.CanBroadcast {
			info.CanSign = true
		}
		info.Keys = append(info.Keys, key)
	}
	return info, nil
}

// CreateVaultTx signs the transaction last proposed for the vault account with a keystore of the
// vault, prompting the user to connect one if needed. The transaction is then pending until a
// second keystore signs it with SignVaultTx().
func (backend *Backend) CreateVaultTx(accountCode accountsTypes.Code, note string) error {
	_, account, err := backend.vaultTxAccount(accountCode)
	if err != nil {
		return err
	}
	return account.CreatePendingMultisigTx(note)
}

// SignVaultTx signs the pending transaction of the vault account with a keystore of the vault,
// prompting the user to connect one if needed. Once two keystores have signed, the transaction is
// sent and its ID is returned. The ID is empty if another signature is needed.
func (backend *Backend) SignVaultTx(accountCode accountsTypes.Code) (string, error) {
	_, account, err := backend.vaultTxAccount(accountCode)
	if err != nil {
		return "", err
	}
	return account.SignPendingMultisigTx()
}

// DiscardVaultTx removes the pending transaction of the vault account.
func (backend *Backend) DiscardVaultTx(accountCode accountsTypes.Code) error {
	_, account, err := backend.vaultTxAccount(accountCode)
	if err != nil {
		return err
	}
	return account.DiscardPendingMultisigTx()
}
