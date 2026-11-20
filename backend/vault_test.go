// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"strings"
	"testing"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/eth"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	keystoremock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore/mocks"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore/software"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/rates"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

var (
	vaultFingerprint1 = []byte{0xa1, 0xa1, 0xa1, 0xa1}
	vaultFingerprint2 = []byte{0xb2, 0xb2, 0xb2, 0xb2}
	vaultFingerprint3 = []byte{0xc3, 0xc3, 0xc3, 0xc3}
)

// vaultTestKeystore mocks a BitBox02 that holds one key of a vault and remembers whether the vault
// has been registered on it.
type vaultTestKeystore struct {
	*keystoremock.KeystoreMock
	helper     *software.Keystore
	registered bool
}

func newVaultTestKeystore(
	t *testing.T,
	name string,
	rootFingerprint []byte,
	seed byte,
) *vaultTestKeystore {
	t.Helper()
	seedBytes := make([]byte, 32)
	seedBytes[0] = seed
	rootKey, err := hdkeychain.NewMaster(seedBytes, &chaincfg.MainNetParams)
	require.NoError(t, err)

	ks := &vaultTestKeystore{
		KeystoreMock: makeBitBox02Multi(),
		helper:       software.NewKeystore(rootKey),
	}
	ks.NameFunc = func() (string, error) { return name, nil }
	ks.RootFingerprintFunc = func() ([]byte, error) { return rootFingerprint, nil }
	ks.ExtendedPublicKeyFunc = ks.helper.ExtendedPublicKey
	ks.BTCXPubsFunc = ks.helper.BTCXPubs
	ks.BTCMultisigRegisteredFunc = func(coinpkg.Coin, *signing.BitcoinMultisig) (bool, error) {
		return ks.registered, nil
	}
	ks.RegisterBTCMultisigFunc = func(coinpkg.Coin, *signing.BitcoinMultisig, string) error {
		ks.registered = true
		return nil
	}
	return ks
}

// vaultKey returns the key the keystore contributes to a vault at the given keypath.
func (ks *vaultTestKeystore) vaultKey(t *testing.T, b *Backend, keypath string) signing.KeyInfo {
	t.Helper()
	coin, err := b.Coin(coinpkg.CodeBTC)
	require.NoError(t, err)
	absoluteKeypath := mustKeypath(keypath)
	xpub, err := ks.helper.ExtendedPublicKey(coin, absoluteKeypath)
	require.NoError(t, err)
	rootFingerprint, err := ks.RootFingerprint()
	require.NoError(t, err)
	return signing.KeyInfo{
		RootFingerprint:   rootFingerprint,
		AbsoluteKeypath:   absoluteKeypath,
		ExtendedPublicKey: xpub,
	}
}

func multisigDescriptor(t *testing.T, threshold int, keys ...signing.KeyInfo) string {
	t.Helper()
	configuration, err := signing.NewBitcoinMultisigConfiguration(threshold, keys)
	require.NoError(t, err)
	descriptor, err := configuration.BitcoinMultisig.Descriptor(&chaincfg.MainNetParams)
	require.NoError(t, err)
	return descriptor
}

type vaultTestSetup struct {
	backend    *Backend
	keystores  [3]*vaultTestKeystore
	keys       [3]signing.KeyInfo
	descriptor string
}

func newVaultTestSetup(t *testing.T) *vaultTestSetup {
	t.Helper()
	setup := &vaultTestSetup{backend: newBackend(t, testnetDisabled, regtestDisabled)}
	t.Cleanup(func() { setup.backend.Close() })
	setup.keystores = [3]*vaultTestKeystore{
		newVaultTestKeystore(t, "BitBox A", vaultFingerprint1, 1),
		newVaultTestKeystore(t, "BitBox B", vaultFingerprint2, 2),
		newVaultTestKeystore(t, "BitBox C", vaultFingerprint3, 3),
	}
	for index, ks := range setup.keystores {
		setup.keys[index] = ks.vaultKey(t, setup.backend, "m/48'/0'/0'/2'")
	}
	setup.descriptor = multisigDescriptor(t, 2, setup.keys[:]...)
	return setup
}

func lookupAccountView(t *testing.T, b *Backend, code accountsTypes.Code) *AccountView {
	t.Helper()
	return b.Accounts().lookup(code)
}

// A vault is added from its descriptor with one of its three keystores connected: the user reviews
// it, it is registered on the keystore under the given name, and it is loaded next to the regular
// accounts of that keystore. The same vault can't be added twice, whatever the order of its keys.
func TestAddVault(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	ks := setup.keystores[0]
	b.registerKeystore(ks)
	checkShownAccountsLen(t, b, 3, 3)

	info, err := b.VaultInfoFromDescriptor(setup.descriptor, ks)
	require.NoError(t, err)
	require.Equal(t, 2, info.Threshold)
	require.False(t, info.Registered)
	require.Len(t, info.Keys, 3)
	for index, key := range info.Keys {
		require.Equal(t, setup.keys[index].RootFingerprint, []byte(key.RootFingerprint))
		require.Equal(t, "m/48'/0'/0'/2'", key.Keypath)
		require.True(t, strings.HasPrefix(key.Xpub, "Zpub"))
	}
	// Only the connected keystore is known to the app at this point.
	require.True(t, info.Keys[0].Connected)
	require.Equal(t, "BitBox A", info.Keys[0].KeystoreName)
	require.False(t, info.Keys[1].Connected)
	require.Empty(t, info.Keys[1].KeystoreName)
	// Reviewing does not register or add anything.
	require.Empty(t, ks.RegisterBTCMultisigCalls())
	checkShownAccountsLen(t, b, 3, 3)

	accountCode, err := b.AddVault(setup.descriptor, "  My vault ", ks)
	require.NoError(t, err)
	require.Equal(t, info.AccountCode, accountCode)
	require.True(t, strings.HasPrefix(string(accountCode), "v0-vault-"))
	require.True(t, strings.HasSuffix(string(accountCode), "-btc"))

	registerCalls := ks.RegisterBTCMultisigCalls()
	require.Len(t, registerCalls, 1)
	require.Equal(t, "My vault", registerCalls[0].Name)
	require.Equal(t, coinpkg.CodeBTC, registerCalls[0].CoinInstance.Code())
	require.Equal(t, 2, registerCalls[0].Multisig.Threshold)

	checkShownAccountsLen(t, b, 4, 4)
	view := lookupAccountView(t, b, accountCode)
	require.NotNil(t, view)
	require.Equal(t, "My vault", view.Record.Name)
	require.Equal(t, coinpkg.CodeBTC, view.Record.CoinCode)
	require.True(t, view.Record.IsMultisig())
	require.Len(t, view.Record.SigningConfigurations, 1)
	require.Equal(t, vaultFingerprint1, []byte(view.Keystore.RootFingerprint))

	info, err = b.VaultInfoFromDescriptor(setup.descriptor, ks)
	require.NoError(t, err)
	require.True(t, info.Registered)

	_, err = b.AddVault(setup.descriptor, "My vault", ks)
	require.Equal(t, errAccountAlreadyExists, errp.Cause(err))
	reordered := multisigDescriptor(t, 2, setup.keys[2], setup.keys[0], setup.keys[1])
	_, err = b.AddVault(reordered, "Other name", ks)
	require.Equal(t, errAccountAlreadyExists, errp.Cause(err))
	require.Len(t, ks.RegisterBTCMultisigCalls(), 1)
	checkShownAccountsLen(t, b, 4, 4)
}

// A vault that is already registered on the keystore, for example when restoring it on another
// computer, is added without asking the user to register it again.
func TestAddVaultAlreadyRegistered(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	ks := setup.keystores[0]
	ks.registered = true
	b.registerKeystore(ks)

	_, err := b.AddVault(setup.descriptor, "My vault", ks)
	require.NoError(t, err)
	require.Empty(t, ks.RegisterBTCMultisigCalls())
	checkShownAccountsLen(t, b, 4, 4)
}

// Nothing is added if the descriptor is not a vault, does not belong to the connected keystore, the
// name can't be stored on the keystore, or the user rejects the registration on the keystore.
func TestAddVaultErrors(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	ks := setup.keystores[0]
	b.registerKeystore(ks)

	otherAccount := setup.keystores[1].vaultKey(t, b, "m/48'/0'/1'/2'")
	singleSigPath := setup.keystores[1].vaultKey(t, b, "m/84'/0'/0'")
	testnetPath := setup.keystores[1].vaultKey(t, b, "m/48'/1'/0'/2'")
	wrappedSegwitPath := setup.keystores[1].vaultKey(t, b, "m/48'/0'/0'/1'")
	accountTooHigh := setup.keystores[1].vaultKey(t, b, "m/48'/0'/100'/2'")
	sameKeystore := otherAccount
	sameKeystore.RootFingerprint = vaultFingerprint1
	// The fingerprint of the connected keystore, with a key the keystore does not derive.
	mismatchedKey := sameKeystore

	tests := []struct {
		name          string
		descriptor    string
		vaultName     string
		expectedError error
	}{
		{
			name:          "empty name",
			descriptor:    setup.descriptor,
			vaultName:     "  ",
			expectedError: errVaultNameInvalid,
		},
		{
			name:          "name too long",
			descriptor:    setup.descriptor,
			vaultName:     strings.Repeat("a", 31),
			expectedError: errVaultNameInvalid,
		},
		{
			name:          "name not ASCII",
			descriptor:    setup.descriptor,
			vaultName:     "Tresor für später",
			expectedError: errVaultNameInvalid,
		},
		{
			name:          "not a descriptor",
			descriptor:    "not a descriptor",
			vaultName:     "My vault",
			expectedError: errVaultDescriptorInvalid,
		},
		{
			name:          "missing checksum",
			descriptor:    strings.Split(setup.descriptor, "#")[0],
			vaultName:     "My vault",
			expectedError: errVaultDescriptorInvalid,
		},
		{
			name:          "1-of-3",
			descriptor:    multisigDescriptor(t, 1, setup.keys[:]...),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "3-of-3",
			descriptor:    multisigDescriptor(t, 3, setup.keys[:]...),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "2-of-2",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], setup.keys[1]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "single-signature keypath",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], singleSigPath, setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "keypath of another coin",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], testnetPath, setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "wrapped segwit keypath",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], wrappedSegwitPath, setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "account number too high",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], accountTooHigh, setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "two keys of one keystore",
			descriptor:    multisigDescriptor(t, 2, setup.keys[0], sameKeystore, setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultUnsupported,
		},
		{
			name:          "connected keystore holds no key",
			descriptor:    multisigDescriptor(t, 2, otherAccount, setup.keys[2], accountKeyOfUnknownKeystore(t, b)),
			vaultName:     "My vault",
			expectedError: errVaultKeystoreNotCosigner,
		},
		{
			name:          "key of connected keystore does not match",
			descriptor:    multisigDescriptor(t, 2, mismatchedKey, setup.keys[1], setup.keys[2]),
			vaultName:     "My vault",
			expectedError: errVaultKeyMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := b.AddVault(test.descriptor, test.vaultName, ks)
			require.Equal(t, test.expectedError, errp.Cause(err))
		})
	}

	t.Run("registration aborted on keystore", func(t *testing.T) {
		ks.RegisterBTCMultisigFunc = func(coinpkg.Coin, *signing.BitcoinMultisig, string) error {
			return errp.WithStack(errp.ErrUserAbort)
		}
		_, err := b.AddVault(setup.descriptor, "My vault", ks)
		require.Equal(t, errp.ErrUserAbort, errp.Cause(err))
	})

	t.Run("keystore does not support vaults", func(t *testing.T) {
		ks.SupportsAccountFunc = func(coinpkg.Coin, interface{}) bool { return false }
		_, err := b.AddVault(setup.descriptor, "My vault", ks)
		require.Equal(t, keystore.ErrUnsupportedFeature, errp.Cause(err))
		_, err = b.VaultInfoFromDescriptor(setup.descriptor, ks)
		require.Equal(t, keystore.ErrUnsupportedFeature, errp.Cause(err))
	})

	checkShownAccountsLen(t, b, 3, 3)
}

// accountKeyOfUnknownKeystore returns a vault key of a keystore that takes no other part in the
// tests.
func accountKeyOfUnknownKeystore(t *testing.T, b *Backend) signing.KeyInfo {
	t.Helper()
	return newVaultTestKeystore(t, "Unknown", []byte{0xd4, 0xd4, 0xd4, 0xd4}, 4).
		vaultKey(t, b, "m/48'/0'/0'/2'")
}

// A vault is loaded with any of its three keystores connected, and with none other. Connecting a
// keystore for the first time still creates its regular accounts, even if it is already part of a
// vault. A vault keeps its single multisig configuration when the accounts of a keystore are
// upgraded.
func TestVaultLoadedWithEachKeystore(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend

	b.registerKeystore(setup.keystores[0])
	accountCode, err := b.AddVault(setup.descriptor, "My vault", setup.keystores[0])
	require.NoError(t, err)
	checkShownAccountsLen(t, b, 4, 4)

	b.DeregisterKeystore()
	checkShownAccountsLen(t, b, 0, 4)

	// The second keystore has never been connected: its three regular accounts are created, and
	// the vault is presented under it.
	b.registerKeystore(setup.keystores[1])
	checkShownAccountsLen(t, b, 4, 7)
	view := lookupAccountView(t, b, accountCode)
	require.NotNil(t, view)
	require.Equal(t, vaultFingerprint2, []byte(view.Keystore.RootFingerprint))
	accountsByKeystore, err := b.AccountsByKeystore()
	require.NoError(t, err)
	require.Len(t, accountsByKeystore, 1)
	require.Len(t, accountsByKeystore["b2b2b2b2"], 4)

	b.DeregisterKeystore()
	b.registerKeystore(setup.keystores[2])
	checkShownAccountsLen(t, b, 4, 10)
	require.NotNil(t, lookupAccountView(t, b, accountCode))

	b.DeregisterKeystore()
	unrelated := newVaultTestKeystore(t, "Unrelated", []byte{0xd4, 0xd4, 0xd4, 0xd4}, 4)
	b.registerKeystore(unrelated)
	checkShownAccountsLen(t, b, 3, 13)
	require.Nil(t, lookupAccountView(t, b, accountCode))

	accountsConfig, err := b.accountsDB.Snapshot()
	require.NoError(t, err)
	record := accountsConfig.Lookup(accountCode)
	require.NotNil(t, record)
	require.Len(t, record.SigningConfigurations, 1)
	require.NotNil(t, record.SigningConfigurations[0].BitcoinMultisig)
}

// A vault stays loaded without a connected keystore if one of its keystores is watch-only. It is
// then presented under that keystore.
func TestVaultWatchonly(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend

	b.registerKeystore(setup.keystores[0])
	accountCode, err := b.AddVault(setup.descriptor, "My vault", setup.keystores[0])
	require.NoError(t, err)
	b.DeregisterKeystore()
	b.registerKeystore(setup.keystores[1])
	require.NoError(t, b.SetWatchonly(vaultFingerprint2, true))
	b.DeregisterKeystore()

	// The regular accounts of the watch-only keystore and the vault remain loaded.
	checkShownAccountsLen(t, b, 4, 7)
	view := lookupAccountView(t, b, accountCode)
	require.NotNil(t, view)
	require.Equal(t, vaultFingerprint2, []byte(view.Keystore.RootFingerprint))

	// With another keystore of the vault connected, the vault is presented under that one.
	b.registerKeystore(setup.keystores[0])
	view = lookupAccountView(t, b, accountCode)
	require.NotNil(t, view)
	require.Equal(t, vaultFingerprint1, []byte(view.Keystore.RootFingerprint))
}

// A keystore of the vault that was not connected when the vault was added registers the vault
// under the name of the account. Registering again does not ask the user a second time.
func TestRegisterVault(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend

	b.registerKeystore(setup.keystores[0])
	accountCode, err := b.AddVault(setup.descriptor, "My vault", setup.keystores[0])
	require.NoError(t, err)
	b.DeregisterKeystore()

	second := setup.keystores[1]
	b.registerKeystore(second)
	require.False(t, second.registered)
	require.NoError(t, b.RegisterVault(accountCode))
	require.True(t, second.registered)
	registerCalls := second.RegisterBTCMultisigCalls()
	require.Len(t, registerCalls, 1)
	require.Equal(t, "My vault", registerCalls[0].Name)

	require.NoError(t, b.RegisterVault(accountCode))
	require.Len(t, second.RegisterBTCMultisigCalls(), 1)

	// Only vault accounts can be registered.
	var regularAccountCode accountsTypes.Code
	for _, view := range b.Accounts() {
		if !view.Record.IsMultisig() {
			regularAccountCode = view.Record.Code
			break
		}
	}
	require.NotEmpty(t, regularAccountCode)
	require.Error(t, b.RegisterVault(regularAccountCode))
	require.Error(t, b.RegisterVault("unknown"))
}

// The account code of a vault depends on its threshold and keys, but not on the order of the keys
// or on their origin metadata.
func TestVaultAccountCode(t *testing.T) {
	setup := newVaultTestSetup(t)
	multisig := func(threshold int, keys ...signing.KeyInfo) *signing.BitcoinMultisig {
		configuration, err := signing.NewBitcoinMultisigConfiguration(threshold, keys)
		require.NoError(t, err)
		return configuration.BitcoinMultisig
	}

	code, err := vaultAccountCode(coinpkg.CodeBTC, multisig(2, setup.keys[:]...))
	require.NoError(t, err)

	reordered, err := vaultAccountCode(
		coinpkg.CodeBTC, multisig(2, setup.keys[1], setup.keys[2], setup.keys[0]))
	require.NoError(t, err)
	require.Equal(t, code, reordered)

	otherOrigin := setup.keys[0]
	otherOrigin.RootFingerprint = []byte{9, 9, 9, 9}
	otherOrigin.AbsoluteKeypath = mustKeypath("m/48'/0'/5'/2'")
	sameKeys, err := vaultAccountCode(
		coinpkg.CodeBTC, multisig(2, otherOrigin, setup.keys[1], setup.keys[2]))
	require.NoError(t, err)
	require.Equal(t, code, sameKeys)

	otherThreshold, err := vaultAccountCode(coinpkg.CodeBTC, multisig(3, setup.keys[:]...))
	require.NoError(t, err)
	require.NotEqual(t, code, otherThreshold)

	otherKey := setup.keystores[0].vaultKey(t, setup.backend, "m/48'/0'/1'/2'")
	otherKeys, err := vaultAccountCode(
		coinpkg.CodeBTC, multisig(2, otherKey, setup.keys[1], setup.keys[2]))
	require.NoError(t, err)
	require.NotEqual(t, code, otherKeys)

	otherCoin, err := vaultAccountCode(coinpkg.CodeTBTC, multisig(2, setup.keys[:]...))
	require.NoError(t, err)
	require.NotEqual(t, code, otherCoin)
}

// On the portfolio, the balance of the vault accounts is summed up separately. It is not part of
// the balance of the keystore the vault is presented under, but it is part of the overall total of
// its coin.
func TestAccountsBalanceSummaryVaults(t *testing.T) {
	withBalances := func(b *Backend) {
		b.makeBtcAccount = func(config *accounts.AccountConfig, coin *btc.Coin, gapLimits *types.GapLimits, getAddress func(coinpkg.Code, blockchain.ScriptHashHex) (*addresses.AccountAddress, error), log *logrus.Entry) accounts.Interface {
			accountMock := MockBtcAccount(t, config, coin, gapLimits, log)
			accountMock.BalanceFunc = func() (*accounts.Balance, error) {
				return accounts.NewBalance(coinpkg.NewAmountFromInt64(1e8), coinpkg.NewAmountFromInt64(0)), nil
			}
			return accountMock
		}
		b.makeEthAccount = func(config *accounts.AccountConfig, coin *eth.Coin, log *logrus.Entry) accounts.Interface {
			accountMock := MockEthAccount(config, coin, log)
			accountMock.BalanceFunc = func() (*accounts.Balance, error) {
				return accounts.NewBalance(coinpkg.NewAmountFromInt64(1e18), coinpkg.NewAmountFromInt64(0)), nil
			}
			return accountMock
		}
	}

	// Without vault accounts there is no vaults balance.
	withoutVault := newVaultTestSetup(t)
	withBalances(withoutVault.backend)
	withoutVault.backend.registerKeystore(withoutVault.keystores[0])
	// The rates are mocked after all changes to the accounts, as these fetch new rates.
	withoutVault.backend.ratesUpdater = rates.MockRateUpdater()
	defer withoutVault.backend.ratesUpdater.Stop()
	summary, err := withoutVault.backend.AccountsBalanceSummary()
	require.NoError(t, err)
	require.Nil(t, summary.VaultsBalance)
	require.Len(t, summary.KeystoresBalance, 1)

	setup := newVaultTestSetup(t)
	b := setup.backend
	withBalances(b)
	b.registerKeystore(setup.keystores[0])
	_, err = b.AddVault(setup.descriptor, "My vault", setup.keystores[0])
	require.NoError(t, err)
	b.ratesUpdater = rates.MockRateUpdater()
	defer b.ratesUpdater.Stop()

	summary, err = b.AccountsBalanceSummary()
	require.NoError(t, err)
	keystoreBalance, ok := summary.KeystoresBalance["a1a1a1a1"]
	require.True(t, ok)
	require.Len(t, summary.KeystoresBalance, 1)
	// The keystore total covers its regular Bitcoin account only.
	require.Equal(t, "1.00000000", keystoreBalance.CoinsBalance[coinpkg.CodeBTC].Amount)
	require.NotNil(t, summary.VaultsBalance)
	require.Equal(t, "1.00000000", summary.VaultsBalance.CoinsBalance[coinpkg.CodeBTC].Amount)
	require.Equal(t, "21.00", summary.VaultsBalance.Total)
	require.Len(t, summary.VaultsBalance.CoinsBalance, 1)
	// The overall Bitcoin total includes the vault.
	foundBitcoinTotal := false
	for _, coinBalance := range summary.CoinsTotalBalance {
		if coinBalance.CoinCode == coinpkg.CodeBTC {
			foundBitcoinTotal = true
			require.Equal(t, "2.00000000", coinBalance.FormattedAmount.Amount)
		}
	}
	require.True(t, foundBitcoinTotal)
}
