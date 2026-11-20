// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	utilcfg "github.com/BitBoxSwiss/bitbox-wallet-app/util/config"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable/action"
)

// A vault is created in three steps. Its three keys are collected from three keystores that are
// connected one after the other. The vault is then registered on each of these keystores, where the
// user compares the keys. Finally the user backs up the descriptor of the vault. The progress is
// persisted, as the user swaps keystores between the steps.

const (
	// errVaultSetupInProgress is returned when starting to create a vault while another one is
	// being created.
	errVaultSetupInProgress errp.ErrorCode = "vaultSetupInProgress"
	// errVaultSetupNotFound is returned if no vault is being created.
	errVaultSetupNotFound errp.ErrorCode = "vaultSetupNotFound"
	// errVaultSetupWrongStep is returned if an action does not apply to the current step.
	errVaultSetupWrongStep errp.ErrorCode = "vaultSetupWrongStep"
	// errVaultKeystoreAlreadyAdded is returned when adding the key of a keystore that already
	// contributed a key.
	errVaultKeystoreAlreadyAdded errp.ErrorCode = "vaultKeystoreAlreadyAdded"
)

// VaultSetupStep is a step of creating a vault.
type VaultSetupStep string

const (
	// VaultSetupStepCollectKeys is the step in which the keys of the vault are collected.
	VaultSetupStepCollectKeys VaultSetupStep = "collectKeys"
	// VaultSetupStepRegister is the step in which the vault is registered on its keystores.
	VaultSetupStepRegister VaultSetupStep = "register"
	// VaultSetupStepBackup is the step in which the user backs up the descriptor of the vault. The
	// vault account exists at this point.
	VaultSetupStepBackup VaultSetupStep = "backup"
)

// VaultSetupKey describes a key collected for the vault that is being created.
type VaultSetupKey struct {
	VaultKey
	// Registered is true if the vault is registered on the keystore holding the key.
	Registered bool `json:"registered"`
}

// VaultSetupInfo describes the vault that is being created.
type VaultSetupInfo struct {
	Name      string          `json:"name"`
	Step      VaultSetupStep  `json:"step"`
	Threshold int             `json:"threshold"`
	NumKeys   int             `json:"numKeys"`
	Keys      []VaultSetupKey `json:"keys"`
	// CanAddKey is true if the connected keystore can contribute the next key.
	CanAddKey bool `json:"canAddKey"`
	// CanRegister is true if the vault can be registered on the connected keystore now.
	CanRegister bool `json:"canRegister"`
	// AccountCode and Descriptor are set in the backup step.
	AccountCode accountsTypes.Code `json:"accountCode,omitempty"`
	Descriptor  string             `json:"descriptor,omitempty"`
}

var vaultFilenameUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func vaultSetupStep(setup *config.VaultSetup) VaultSetupStep {
	if len(setup.Keys) < vaultNumKeys {
		return VaultSetupStepCollectKeys
	}
	for _, key := range setup.Keys {
		if !setup.IsRegistered(key.RootFingerprint) {
			return VaultSetupStepRegister
		}
	}
	return VaultSetupStepBackup
}

func vaultSetupKeyIndex(setup *config.VaultSetup, rootFingerprint []byte) int {
	for index, key := range setup.Keys {
		if bytes.Equal(key.RootFingerprint, rootFingerprint) {
			return index
		}
	}
	return -1
}

func (backend *Backend) notifyVaultSetupChanged() {
	backend.Notify(observable.Event{
		Subject: "vault/setup",
		Action:  action.Reload,
	})
}

func (backend *Backend) btcCoin(coinCode coinpkg.Code) (*btc.Coin, error) {
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

// VaultSupported returns true if the keystore can hold a key of a vault.
func (backend *Backend) VaultSupported(ks keystore.Keystore) bool {
	coin, err := backend.vaultCoin()
	if err != nil {
		return false
	}
	return ks.SupportsAccount(coin, signing.ScriptTypeP2WSH)
}

// nextVaultAccountNumber returns the lowest BIP-48 account number of the keystore that is not used
// by any of its vaults yet.
func nextVaultAccountNumber(
	accountsConfig *config.AccountsConfig,
	coinCode coinpkg.Code,
	rootFingerprint []byte,
) (uint32, error) {
	next := uint32(0)
	for _, account := range accountsConfig.Accounts {
		multisig := account.SigningConfigurations.Multisig()
		if multisig == nil || account.CoinCode != coinCode {
			continue
		}
		for _, key := range multisig.Keys {
			if !bytes.Equal(key.RootFingerprint, rootFingerprint) {
				continue
			}
			keypath := key.AbsoluteKeypath.ToUInt32()
			if len(keypath) != 4 || keypath[2] < hardenedKeystart {
				continue
			}
			if accountNumber := keypath[2] - hardenedKeystart; accountNumber+1 > next {
				next = accountNumber + 1
			}
		}
	}
	if next > bip48MaxAccountNumber {
		return 0, errp.WithStack(errAccountLimitReached)
	}
	return next, nil
}

// vaultSetupKey fetches the key the keystore contributes to a new vault.
func vaultSetupKey(
	coin *btc.Coin,
	ks keystore.Keystore,
	accountsConfig *config.AccountsConfig,
) (*signing.KeyInfo, error) {
	if !ks.SupportsAccount(coin, signing.ScriptTypeP2WSH) {
		return nil, errp.WithStack(keystore.ErrUnsupportedFeature)
	}
	rootFingerprint, err := ks.RootFingerprint()
	if err != nil {
		return nil, err
	}
	coinType, ok := coinpkg.BIP44CoinType(coin.Code())
	if !ok {
		return nil, errp.Newf("Unrecognized coin code: %s", coin.Code())
	}
	accountNumber, err := nextVaultAccountNumber(accountsConfig, coin.Code(), rootFingerprint)
	if err != nil {
		return nil, err
	}
	keypath := signing.NewAbsoluteKeypathFromUint32(
		bip48Purpose+hardenedKeystart,
		coinType+hardenedKeystart,
		accountNumber+hardenedKeystart,
		bip48ScriptTypeP2WSH+hardenedKeystart,
	)
	xpub, err := ks.ExtendedPublicKey(coin, keypath)
	if err != nil {
		return nil, err
	}
	return &signing.KeyInfo{
		RootFingerprint:   rootFingerprint,
		AbsoluteKeypath:   keypath,
		ExtendedPublicKey: xpub,
	}, nil
}

// VaultSetupInfo returns the vault that is being created, or nil if none is.
func (backend *Backend) VaultSetupInfo() (*VaultSetupInfo, error) {
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return nil, err
	}
	setup := accountsConfig.VaultSetup
	if setup == nil {
		return nil, nil
	}
	coin, err := backend.btcCoin(setup.CoinCode)
	if err != nil {
		return nil, err
	}

	var connectedRootFingerprint []byte
	connectedKeystore := backend.Keystore()
	if connectedKeystore != nil {
		connectedRootFingerprint, err = connectedKeystore.RootFingerprint()
		if err != nil {
			return nil, err
		}
	}

	step := vaultSetupStep(setup)
	info := &VaultSetupInfo{
		Name:      setup.Name,
		Step:      step,
		Threshold: vaultThreshold,
		NumKeys:   vaultNumKeys,
		Keys:      make([]VaultSetupKey, len(setup.Keys)),
	}
	connectedKeyIndex := -1
	for index, key := range setup.Keys {
		xpub, err := vaultDisplayXpub(coin, key.ExtendedPublicKey)
		if err != nil {
			return nil, err
		}
		keystoreName := ""
		if persistedKeystore, err := accountsConfig.LookupKeystore(key.RootFingerprint); err == nil {
			keystoreName = persistedKeystore.Name
		}
		connected := connectedKeystore != nil &&
			bytes.Equal(key.RootFingerprint, connectedRootFingerprint)
		if connected {
			connectedKeyIndex = index
		}
		info.Keys[index] = VaultSetupKey{
			VaultKey: VaultKey{
				RootFingerprint: key.RootFingerprint,
				Keypath:         key.AbsoluteKeypath.Encode(),
				Xpub:            xpub,
				KeystoreName:    keystoreName,
				Connected:       connected,
			},
			Registered: setup.IsRegistered(key.RootFingerprint),
		}
	}

	switch step {
	case VaultSetupStepCollectKeys:
		info.CanAddKey = connectedKeystore != nil &&
			connectedKeyIndex == -1 &&
			connectedKeystore.SupportsAccount(coin, signing.ScriptTypeP2WSH)
	case VaultSetupStepRegister:
		info.CanRegister = connectedKeyIndex != -1 && !info.Keys[connectedKeyIndex].Registered
	case VaultSetupStepBackup:
		multisig := &signing.BitcoinMultisig{Threshold: vaultThreshold, Keys: setup.Keys}
		info.AccountCode, err = vaultAccountCode(setup.CoinCode, multisig)
		if err != nil {
			return nil, err
		}
		info.Descriptor, err = multisig.Descriptor(coin.Net())
		if err != nil {
			return nil, err
		}
	}
	return info, nil
}

// StartVaultSetup starts creating a vault with the given name. The keystore contributes the first
// key.
func (backend *Backend) StartVaultSetup(name string, ks keystore.Keystore) error {
	name, err := validateVaultName(name)
	if err != nil {
		return err
	}
	coin, err := backend.vaultCoin()
	if err != nil {
		return err
	}
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return err
	}
	if accountsConfig.VaultSetup != nil {
		return errp.WithStack(errVaultSetupInProgress)
	}
	// The key is fetched from the keystore before updating the database.
	key, err := vaultSetupKey(coin, ks, &accountsConfig)
	if err != nil {
		return err
	}
	err = backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		if accountsConfig.VaultSetup != nil {
			return errp.WithStack(errVaultSetupInProgress)
		}
		accountsConfig.VaultSetup = &config.VaultSetup{
			CoinCode: coin.Code(),
			Name:     name,
			Keys:     []signing.KeyInfo{*key},
		}
		return nil
	})
	if err != nil {
		return err
	}
	backend.notifyVaultSetupChanged()
	return nil
}

// AddVaultSetupKey adds the key of the keystore to the vault that is being created. Every keystore
// contributes one key.
func (backend *Backend) AddVaultSetupKey(ks keystore.Keystore) error {
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return err
	}
	setup := accountsConfig.VaultSetup
	if setup == nil {
		return errp.WithStack(errVaultSetupNotFound)
	}
	if vaultSetupStep(setup) != VaultSetupStepCollectKeys {
		return errp.WithStack(errVaultSetupWrongStep)
	}
	coin, err := backend.btcCoin(setup.CoinCode)
	if err != nil {
		return err
	}
	// The key is fetched from the keystore before updating the database.
	key, err := vaultSetupKey(coin, ks, &accountsConfig)
	if err != nil {
		return err
	}
	if vaultSetupKeyIndex(setup, key.RootFingerprint) != -1 {
		return errp.WithStack(errVaultKeystoreAlreadyAdded)
	}
	numKeys := len(setup.Keys)
	err = backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		setup := accountsConfig.VaultSetup
		if setup == nil {
			return errp.WithStack(errVaultSetupNotFound)
		}
		if len(setup.Keys) != numKeys {
			return errp.WithStack(errVaultSetupWrongStep)
		}
		if vaultSetupKeyIndex(setup, key.RootFingerprint) != -1 {
			return errp.WithStack(errVaultKeystoreAlreadyAdded)
		}
		keys := append(setup.Keys, *key)
		if len(keys) == vaultNumKeys {
			multisig := &signing.BitcoinMultisig{Threshold: vaultThreshold, Keys: keys}
			if err := validateVault(setup.CoinCode, multisig); err != nil {
				return err
			}
		}
		setup.Keys = keys
		return nil
	})
	if err != nil {
		return err
	}
	backend.notifyVaultSetupChanged()
	return nil
}

// RegisterVaultSetup registers the vault that is being created on the keystore, which must hold one
// of its keys. The user confirms the name and all keys on the keystore. The vault account is added
// once the vault is registered on all of its keystores.
func (backend *Backend) RegisterVaultSetup(ks keystore.Keystore) error {
	accountsConfig, err := backend.accountsDB.Snapshot()
	if err != nil {
		return err
	}
	setup := accountsConfig.VaultSetup
	if setup == nil {
		return errp.WithStack(errVaultSetupNotFound)
	}
	if vaultSetupStep(setup) != VaultSetupStepRegister {
		return errp.WithStack(errVaultSetupWrongStep)
	}
	coin, err := backend.btcCoin(setup.CoinCode)
	if err != nil {
		return err
	}
	multisig := &signing.BitcoinMultisig{Threshold: vaultThreshold, Keys: setup.Keys}
	if err := validateVault(setup.CoinCode, multisig); err != nil {
		return err
	}
	if err := checkVaultKeystore(coin, multisig, ks); err != nil {
		return err
	}
	accountCode, err := vaultAccountCode(setup.CoinCode, multisig)
	if err != nil {
		return err
	}
	rootFingerprint, err := ks.RootFingerprint()
	if err != nil {
		return err
	}

	// The user confirms the registration on the keystore, which can take a while. This happens
	// before acquiring accountsAndKeystoreLock so that the accounts remain accessible meanwhile.
	if err := registerVaultOnKeystore(coin, multisig, setup.Name, ks); err != nil {
		return err
	}

	defer backend.accountsAndKeystoreLock.Lock()()
	accountAdded := false
	err = backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		setup := accountsConfig.VaultSetup
		if setup == nil {
			return errp.WithStack(errVaultSetupNotFound)
		}
		if vaultSetupKeyIndex(setup, rootFingerprint) == -1 {
			return errp.WithStack(errVaultKeystoreNotCosigner)
		}
		if !setup.IsRegistered(rootFingerprint) {
			setup.Registered = append(setup.Registered, rootFingerprint)
		}
		if vaultSetupStep(setup) != VaultSetupStepBackup || accountsConfig.Lookup(accountCode) != nil {
			return nil
		}
		accountAdded = true
		return backend.persistAccount(config.Account{
			CoinCode: setup.CoinCode,
			Name:     setup.Name,
			Code:     accountCode,
			SigningConfigurations: signing.Configurations{
				{BitcoinMultisig: &signing.BitcoinMultisig{
					Threshold: vaultThreshold,
					Keys:      setup.Keys,
				}},
			},
		}, accountsConfig)
	})
	if err != nil {
		return err
	}
	backend.notifyVaultSetupChanged()
	if accountAdded {
		return backend.reconcileAccountWriteLocked(accountCode)
	}
	return nil
}

// FinishVaultSetup completes creating a vault, after the user backed up its descriptor. It returns
// the code of the vault account.
func (backend *Backend) FinishVaultSetup() (accountsTypes.Code, error) {
	var accountCode accountsTypes.Code
	err := backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		setup := accountsConfig.VaultSetup
		if setup == nil {
			return errp.WithStack(errVaultSetupNotFound)
		}
		if vaultSetupStep(setup) != VaultSetupStepBackup {
			return errp.WithStack(errVaultSetupWrongStep)
		}
		var err error
		accountCode, err = vaultAccountCode(
			setup.CoinCode,
			&signing.BitcoinMultisig{Threshold: vaultThreshold, Keys: setup.Keys},
		)
		if err != nil {
			return err
		}
		accountsConfig.VaultSetup = nil
		return nil
	})
	if err != nil {
		return "", err
	}
	backend.notifyVaultSetupChanged()
	return accountCode, nil
}

// DiscardVaultSetup stops creating a vault. A vault that is already registered on all of its
// keystores remains available as an account.
func (backend *Backend) DiscardVaultSetup() error {
	err := backend.accountsDB.Update(func(accountsConfig *config.AccountsConfig) error {
		accountsConfig.VaultSetup = nil
		return nil
	})
	if err != nil {
		return err
	}
	backend.notifyVaultSetupChanged()
	return nil
}

// ExportVaultDescriptor writes the descriptor of a vault account to a file chosen by the user.
// The descriptor is needed, together with two of the keystores, to recover the funds of the vault.
func (backend *Backend) ExportVaultDescriptor(accountCode accountsTypes.Code) error {
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
	coin, err := backend.btcCoin(account.CoinCode)
	if err != nil {
		return err
	}
	descriptor, err := multisig.Descriptor(coin.Net())
	if err != nil {
		return err
	}

	exportsDir, err := utilcfg.ExportsDir()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("vault-%s.txt", vaultFilenameUnsafeChars.ReplaceAllString(account.Name, "-"))
	path := backend.Environment().GetSaveFilename(filepath.Join(exportsDir, name))
	if path == "" {
		return errp.ErrUserAbort
	}
	// The descriptor reveals all addresses of the vault.
	if err := os.WriteFile(path, []byte(descriptor+"\n"), utilcfg.PrivateFileMode); err != nil {
		return errp.WithStack(err)
	}
	if err := utilcfg.EnsurePrivateFile(path); err != nil {
		return err
	}
	if runtime.GOOS == "android" || runtime.GOOS == "ios" {
		return backend.environment.SystemOpen(path)
	}
	return nil
}
