// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"
	"strings"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/jsonp"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
)

// A vault is a Bitcoin account that requires the signatures of two out of three keystores to spend.
// It is a P2WSH multisig account described by wsh(sortedmulti(2,...)), with every key at the BIP-48
// keypath m/48'/coin'/account'/2'.

const (
	vaultThreshold = 2
	vaultNumKeys   = 3
	// vaultNameMaxLen is the maximum length of a vault name. The BitBox02 stores the name of a
	// registered multisig account and limits it to this length.
	vaultNameMaxLen = 30

	bip48Purpose         = 48
	bip48ScriptTypeP2WSH = 2
	// bip48MaxAccountNumber is the highest account number the BitBox02 accepts in a keypath.
	bip48MaxAccountNumber = 99
)

const (
	// errVaultDescriptorInvalid is returned if a descriptor can't be read as a multisig descriptor.
	errVaultDescriptorInvalid errp.ErrorCode = "vaultDescriptorInvalid"
	// errVaultUnsupported is returned if a multisig descriptor does not describe a vault.
	errVaultUnsupported errp.ErrorCode = "vaultUnsupported"
	// errVaultKeystoreNotCosigner is returned if the keystore does not hold a key of the vault.
	errVaultKeystoreNotCosigner errp.ErrorCode = "vaultKeystoreNotCosigner"
	// errVaultKeyMismatch is returned if the keystore derives a different key than the one the
	// vault states for it.
	errVaultKeyMismatch errp.ErrorCode = "vaultKeyMismatch"
	// errVaultNameInvalid is returned if a vault name can't be stored on the keystore.
	errVaultNameInvalid errp.ErrorCode = "vaultNameInvalid"
)

// VaultKey describes one of the keys of a vault.
type VaultKey struct {
	RootFingerprint jsonp.HexBytes `json:"rootFingerprint"`
	Keypath         string         `json:"keypath"`
	// Xpub is the account-level extended public key, encoded like the BitBox02 displays it when
	// registering the vault.
	Xpub string `json:"xpub"`
	// KeystoreName is the name of the keystore holding the key. It is empty if the keystore has
	// never been connected.
	KeystoreName string `json:"keystoreName"`
	// Connected is true if the key is held by the connected keystore.
	Connected bool `json:"connected"`
}

// VaultInfo describes a vault read from its descriptor.
type VaultInfo struct {
	AccountCode accountsTypes.Code `json:"accountCode"`
	Threshold   int                `json:"threshold"`
	Keys        []VaultKey         `json:"keys"`
	// Registered is true if the vault is registered on the connected keystore.
	Registered bool `json:"registered"`
}

// vaultCoin returns the coin of vault accounts in the current network mode.
func (backend *Backend) vaultCoin() (*btc.Coin, error) {
	coinCode := coinpkg.CodeBTC
	switch {
	case backend.arguments.Regtest():
		coinCode = coinpkg.CodeRBTC
	case backend.Testing():
		coinCode = coinpkg.CodeTBTC
	}
	coin, err := backend.Coin(coinCode)
	if err != nil {
		return nil, err
	}
	btcCoin, ok := coin.(*btc.Coin)
	if !ok {
		return nil, errp.Newf("unexpected coin type for %s", coinCode)
	}
	return btcCoin, nil
}

// validateVaultName returns the name without surrounding whitespace. It must be 1 to 30 printable
// ASCII characters so that every BitBox02 can store and display it.
func validateVaultName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > vaultNameMaxLen {
		return "", errp.WithStack(errVaultNameInvalid)
	}
	for _, char := range name {
		if char < ' ' || char > '~' {
			return "", errp.WithStack(errVaultNameInvalid)
		}
	}
	return name, nil
}

// validateVault checks that a multisig configuration is a vault of the given coin: 2-of-3, with
// every key at a BIP-48 P2WSH keypath of the coin, and every key held by a different keystore.
func validateVault(coinCode coinpkg.Code, multisig *signing.BitcoinMultisig) error {
	if err := multisig.Validate(); err != nil {
		return err
	}
	if multisig.Threshold != vaultThreshold || len(multisig.Keys) != vaultNumKeys {
		return errp.Newf(
			"a vault is %d-of-%d, got %d-of-%d",
			vaultThreshold, vaultNumKeys, multisig.Threshold, len(multisig.Keys))
	}
	coinType, ok := coinpkg.BIP44CoinType(coinCode)
	if !ok {
		return errp.Newf("Unrecognized coin code: %s", coinCode)
	}
	for index, key := range multisig.Keys {
		keypath := key.AbsoluteKeypath.ToUInt32()
		if len(keypath) != 4 ||
			keypath[0] != bip48Purpose+hardenedKeystart ||
			keypath[1] != coinType+hardenedKeystart ||
			keypath[2] < hardenedKeystart ||
			keypath[2] > bip48MaxAccountNumber+hardenedKeystart ||
			keypath[3] != bip48ScriptTypeP2WSH+hardenedKeystart {
			return errp.Newf("unexpected vault keypath: %s", key.AbsoluteKeypath.Encode())
		}
		for _, otherKey := range multisig.Keys[:index] {
			if bytes.Equal(key.RootFingerprint, otherKey.RootFingerprint) {
				return errp.New("two keys of the vault are held by the same keystore")
			}
		}
	}
	return nil
}

// parseVault reads the descriptor of a vault of the given coin.
func parseVault(coin *btc.Coin, descriptor string) (*signing.BitcoinMultisig, error) {
	multisig, err := signing.ParseBitcoinMultisigDescriptor(descriptor, coin.Net())
	if err != nil {
		return nil, errp.Wrap(errVaultDescriptorInvalid, err.Error())
	}
	if err := validateVault(coin.Code(), multisig); err != nil {
		return nil, errp.Wrap(errVaultUnsupported, err.Error())
	}
	return multisig, nil
}

// checkVaultKeystore verifies that the keystore can be used with the vault: it supports multisig
// accounts, holds one of the keys, and derives that key at the keypath the vault states for it.
func checkVaultKeystore(
	coin *btc.Coin,
	multisig *signing.BitcoinMultisig,
	ks keystore.Keystore,
) error {
	if !ks.SupportsAccount(coin, signing.ScriptTypeP2WSH) {
		return errp.WithStack(keystore.ErrUnsupportedFeature)
	}
	rootFingerprint, err := ks.RootFingerprint()
	if err != nil {
		return err
	}
	keyIndex := multisig.KeyIndex(rootFingerprint)
	if keyIndex == -1 {
		return errp.WithStack(errVaultKeystoreNotCosigner)
	}
	key := multisig.Keys[keyIndex]
	keystoreXpub, err := ks.ExtendedPublicKey(coin, key.AbsoluteKeypath)
	if err != nil {
		return err
	}
	same, err := signing.SameExtendedPublicKey(keystoreXpub, key.ExtendedPublicKey)
	if err != nil {
		return err
	}
	if !same {
		return errp.WithStack(errVaultKeyMismatch)
	}
	return nil
}

// registerVaultOnKeystore registers the vault on the keystore unless it is registered already.
// The user confirms the name and all keys on the keystore.
func registerVaultOnKeystore(
	coin *btc.Coin,
	multisig *signing.BitcoinMultisig,
	name string,
	keystore keystore.Keystore,
) error {
	registered, err := keystore.BTCMultisigRegistered(coin, multisig)
	if err != nil {
		return err
	}
	if registered {
		return nil
	}
	return keystore.RegisterBTCMultisig(coin, multisig, name)
}

// vaultDisplayXpub encodes a vault key like the BitBox02 displays it when registering the vault.
func vaultDisplayXpub(coin *btc.Coin, xpub *hdkeychain.ExtendedKey) (string, error) {
	xpubCopy, err := hdkeychain.NewKeyFromString(xpub.String())
	if err != nil {
		return "", errp.WithStack(err)
	}
	xpubCopy.SetNet(&chaincfg.Params{HDPublicKeyID: btc.XPubVersionForMultisig(coin)})
	return xpubCopy.String(), nil
}

// VaultInfoFromDescriptor reads a vault from its descriptor, for the user to review before adding
// it with AddVault(). The keystore must hold one of the keys of the vault.
func (backend *Backend) VaultInfoFromDescriptor(
	descriptor string,
	keystore keystore.Keystore,
) (*VaultInfo, error) {
	coin, err := backend.vaultCoin()
	if err != nil {
		return nil, err
	}
	multisig, err := parseVault(coin, descriptor)
	if err != nil {
		return nil, err
	}
	if err := checkVaultKeystore(coin, multisig, keystore); err != nil {
		return nil, err
	}
	accountCode, err := vaultAccountCode(coin.Code(), multisig)
	if err != nil {
		return nil, err
	}
	registered, err := keystore.BTCMultisigRegistered(coin, multisig)
	if err != nil {
		return nil, err
	}
	connectedRootFingerprint, err := keystore.RootFingerprint()
	if err != nil {
		return nil, err
	}
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return nil, err
	}

	keys := make([]VaultKey, len(multisig.Keys))
	for index, key := range multisig.Keys {
		xpub, err := vaultDisplayXpub(coin, key.ExtendedPublicKey)
		if err != nil {
			return nil, err
		}
		keystoreName := ""
		if persistedKeystore, err := accountsConfig.LookupKeystore(key.RootFingerprint); err == nil {
			keystoreName = persistedKeystore.Name
		}
		keys[index] = VaultKey{
			RootFingerprint: key.RootFingerprint,
			Keypath:         key.AbsoluteKeypath.Encode(),
			Xpub:            xpub,
			KeystoreName:    keystoreName,
			Connected:       bytes.Equal(key.RootFingerprint, connectedRootFingerprint),
		}
	}
	return &VaultInfo{
		AccountCode: accountCode,
		Threshold:   multisig.Threshold,
		Keys:        keys,
		Registered:  registered,
	}, nil
}

// AddVault adds the vault described by the descriptor as an account. The keystore must hold one of
// the keys of the vault. The vault is registered on the keystore first, unless it is registered
// already: the user confirms the name and all keys on the keystore. The account is only added once
// the vault is registered.
func (backend *Backend) AddVault(
	descriptor string,
	name string,
	keystore keystore.Keystore,
) (accountsTypes.Code, error) {
	name, err := validateVaultName(name)
	if err != nil {
		return "", err
	}
	coin, err := backend.vaultCoin()
	if err != nil {
		return "", err
	}
	multisig, err := parseVault(coin, descriptor)
	if err != nil {
		return "", err
	}
	if err := checkVaultKeystore(coin, multisig, keystore); err != nil {
		return "", err
	}
	accountCode, err := vaultAccountCode(coin.Code(), multisig)
	if err != nil {
		return "", err
	}
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return "", err
	}
	if accountsConfig.Lookup(accountCode) != nil {
		return "", errp.WithStack(errAccountAlreadyExists)
	}

	// The user confirms the registration on the keystore, which can take a while. This happens
	// before acquiring accountsAndKeystoreLock so that the accounts remain accessible meanwhile.
	if err := registerVaultOnKeystore(coin, multisig, name, keystore); err != nil {
		return "", err
	}

	defer backend.accountsAndKeystoreLock.Lock()()
	account := config.Account{
		CoinCode: coin.Code(),
		Name:     name,
		Code:     accountCode,
		SigningConfigurations: signing.Configurations{
			{BitcoinMultisig: multisig},
		},
	}
	if err := backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		return backend.persistAccount(account, accountsConfig)
	}); err != nil {
		return "", err
	}
	if err := backend.reconcileAccountWriteLocked(accountCode); err != nil {
		return "", err
	}
	return accountCode, nil
}

// RegisterVault registers an existing vault account on the connected keystore, prompting the user
// to connect one of the keystores of the vault if necessary. This is needed once for every keystore
// of the vault, before the keystore can display addresses of the vault.
func (backend *Backend) RegisterVault(accountCode accountsTypes.Code) error {
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return err
	}
	account := accountsConfig.Lookup(accountCode)
	if account == nil {
		return errp.Newf("Could not find account %s", accountCode)
	}
	multisig := account.SigningConfigurations.Multisig()
	if multisig == nil {
		return errp.Newf("Account %s is not a vault", accountCode)
	}
	coin, err := backend.Coin(account.CoinCode)
	if err != nil {
		return err
	}
	btcCoin, ok := coin.(*btc.Coin)
	if !ok {
		return errp.Newf("unexpected coin type for %s", account.CoinCode)
	}
	name, err := validateVaultName(account.Name)
	if err != nil {
		return err
	}
	keystore, err := backend.ConnectKeystoreAnyOf(multisig.RootFingerprints())
	if err != nil {
		return err
	}
	if err := checkVaultKeystore(btcCoin, multisig, keystore); err != nil {
		return err
	}
	return registerVaultOnKeystore(btcCoin, multisig, name, keystore)
}
