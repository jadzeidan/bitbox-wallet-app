// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"testing"

	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/stretchr/testify/require"
)

func mustVaultSetupInfo(t *testing.T, b *Backend) *VaultSetupInfo {
	t.Helper()
	info, err := b.VaultSetupInfo()
	require.NoError(t, err)
	return info
}

// A vault is created by collecting one key from each of three keystores connected one after the
// other, then registering the vault on each of them. The account only exists once the vault is
// registered on all three, and the setup ends after the backup step.
func TestVaultSetup(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	first, second, third := setup.keystores[0], setup.keystores[1], setup.keystores[2]

	b.registerKeystore(first)
	require.True(t, b.VaultSupported(first))
	require.Nil(t, mustVaultSetupInfo(t, b))

	// The connected keystore contributes the first key.
	require.NoError(t, b.StartVaultSetup(" My vault ", first))
	info := mustVaultSetupInfo(t, b)
	require.Equal(t, "My vault", info.Name)
	require.Equal(t, VaultSetupStepCollectKeys, info.Step)
	require.Equal(t, 2, info.Threshold)
	require.Equal(t, 3, info.NumKeys)
	require.Len(t, info.Keys, 1)
	require.Equal(t, vaultFingerprint1, []byte(info.Keys[0].RootFingerprint))
	require.Equal(t, "m/48'/0'/0'/2'", info.Keys[0].Keypath)
	require.Equal(t, "BitBox A", info.Keys[0].KeystoreName)
	require.True(t, info.Keys[0].Connected)
	require.False(t, info.Keys[0].Registered)
	require.False(t, info.CanAddKey)
	require.False(t, info.CanRegister)

	require.Equal(t, errVaultSetupInProgress, errp.Cause(b.StartVaultSetup("Other", first)))
	require.Equal(t, errVaultKeystoreAlreadyAdded, errp.Cause(b.AddVaultSetupKey(first)))

	// The setup survives disconnecting the keystore.
	b.DeregisterKeystore()
	info = mustVaultSetupInfo(t, b)
	require.False(t, info.Keys[0].Connected)
	require.False(t, info.CanAddKey)

	b.registerKeystore(second)
	require.True(t, mustVaultSetupInfo(t, b).CanAddKey)
	require.NoError(t, b.AddVaultSetupKey(second))
	info = mustVaultSetupInfo(t, b)
	require.Len(t, info.Keys, 2)
	require.Equal(t, VaultSetupStepCollectKeys, info.Step)
	require.False(t, info.CanAddKey)
	// The vault can only be registered once all keys are known.
	require.Equal(t, errVaultSetupWrongStep, errp.Cause(b.RegisterVaultSetup(second)))

	b.DeregisterKeystore()
	b.registerKeystore(third)
	require.NoError(t, b.AddVaultSetupKey(third))
	info = mustVaultSetupInfo(t, b)
	require.Len(t, info.Keys, 3)
	require.Equal(t, VaultSetupStepRegister, info.Step)
	require.True(t, info.CanRegister)
	require.Empty(t, info.Descriptor)
	require.Equal(t, errVaultSetupWrongStep, errp.Cause(b.AddVaultSetupKey(third)))

	// The third keystore registers first, as it is connected when the keys are complete.
	require.NoError(t, b.RegisterVaultSetup(third))
	require.True(t, third.registered)
	require.Equal(t, "My vault", third.RegisterBTCMultisigCalls()[0].Name)
	info = mustVaultSetupInfo(t, b)
	require.Equal(t, VaultSetupStepRegister, info.Step)
	require.True(t, info.Keys[2].Registered)
	require.False(t, info.CanRegister)
	_, err := b.FinishVaultSetup()
	require.Equal(t, errVaultSetupWrongStep, errp.Cause(err))

	b.DeregisterKeystore()
	b.registerKeystore(second)
	require.True(t, mustVaultSetupInfo(t, b).CanRegister)
	require.NoError(t, b.RegisterVaultSetup(second))
	// No account yet: the vault is not registered on the first keystore.
	checkShownAccountsLen(t, b, 3, 9)

	b.DeregisterKeystore()
	b.registerKeystore(first)
	require.NoError(t, b.RegisterVaultSetup(first))
	info = mustVaultSetupInfo(t, b)
	require.Equal(t, VaultSetupStepBackup, info.Step)
	require.Equal(t, setup.descriptor, info.Descriptor)
	require.NotEmpty(t, info.AccountCode)
	for _, key := range info.Keys {
		require.True(t, key.Registered)
	}
	checkShownAccountsLen(t, b, 4, 10)
	view := lookupAccountView(t, b, info.AccountCode)
	require.NotNil(t, view)
	require.Equal(t, "My vault", view.Record.Name)
	require.True(t, view.Record.IsMultisig())
	require.Equal(t, errVaultSetupWrongStep, errp.Cause(b.RegisterVaultSetup(first)))

	accountCode, err := b.FinishVaultSetup()
	require.NoError(t, err)
	require.Equal(t, info.AccountCode, accountCode)
	require.Nil(t, mustVaultSetupInfo(t, b))
	checkShownAccountsLen(t, b, 4, 10)
	_, err = b.FinishVaultSetup()
	require.Equal(t, errVaultSetupNotFound, errp.Cause(err))
}

// A keystore contributes a fresh BIP-48 account to every vault, so that two vaults never share a
// key.
func TestVaultSetupAccountNumber(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	first, second := setup.keystores[0], setup.keystores[1]

	b.registerKeystore(first)
	_, err := b.AddVault(setup.descriptor, "First vault", first)
	require.NoError(t, err)

	require.NoError(t, b.StartVaultSetup("Second vault", first))
	require.Equal(t, "m/48'/0'/1'/2'", mustVaultSetupInfo(t, b).Keys[0].Keypath)

	b.DeregisterKeystore()
	b.registerKeystore(second)
	require.NoError(t, b.AddVaultSetupKey(second))
	require.Equal(t, "m/48'/0'/1'/2'", mustVaultSetupInfo(t, b).Keys[1].Keypath)

	b.DeregisterKeystore()
	unrelated := newVaultTestKeystore(t, "Unrelated", []byte{0xd4, 0xd4, 0xd4, 0xd4}, 4)
	b.registerKeystore(unrelated)
	require.NoError(t, b.AddVaultSetupKey(unrelated))
	require.Equal(t, "m/48'/0'/0'/2'", mustVaultSetupInfo(t, b).Keys[2].Keypath)
}

// A setup can be discarded at any time, after which a new one can be started. Nothing is started
// with an invalid name or a keystore that does not support vaults, and a registration the user
// rejects on the keystore does not count.
func TestVaultSetupDiscardAndErrors(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	first, second, third := setup.keystores[0], setup.keystores[1], setup.keystores[2]
	b.registerKeystore(first)

	require.Equal(t, errVaultSetupNotFound, errp.Cause(b.AddVaultSetupKey(first)))
	require.Equal(t, errVaultSetupNotFound, errp.Cause(b.RegisterVaultSetup(first)))
	require.Equal(t, errVaultNameInvalid, errp.Cause(b.StartVaultSetup("", first)))
	require.Nil(t, mustVaultSetupInfo(t, b))

	require.NoError(t, b.StartVaultSetup("My vault", first))
	require.NoError(t, b.DiscardVaultSetup())
	require.Nil(t, mustVaultSetupInfo(t, b))
	require.NoError(t, b.DiscardVaultSetup())

	unsupported := newVaultTestKeystore(t, "Unsupported", []byte{0xd4, 0xd4, 0xd4, 0xd4}, 4)
	unsupported.SupportsAccountFunc = func(coinpkg.Coin, interface{}) bool { return false }
	require.False(t, b.VaultSupported(unsupported))
	require.Equal(t,
		keystore.ErrUnsupportedFeature, errp.Cause(b.StartVaultSetup("My vault", unsupported)))

	require.NoError(t, b.StartVaultSetup("My vault", first))
	require.NoError(t, b.AddVaultSetupKey(second))
	require.NoError(t, b.AddVaultSetupKey(third))
	third.RegisterBTCMultisigFunc = func(coinpkg.Coin, *signing.BitcoinMultisig, string) error {
		return errp.WithStack(errp.ErrUserAbort)
	}
	require.Equal(t, errp.ErrUserAbort, errp.Cause(b.RegisterVaultSetup(third)))
	require.False(t, mustVaultSetupInfo(t, b).Keys[2].Registered)
	// A keystore that holds no key of the vault can't register it.
	unrelated := newVaultTestKeystore(t, "Unrelated", []byte{0xe5, 0xe5, 0xe5, 0xe5}, 5)
	require.Equal(t, errVaultKeystoreNotCosigner, errp.Cause(b.RegisterVaultSetup(unrelated)))

	// The setup is part of the persisted accounts configuration.
	accountsConfig, err := b.accountsDB.Snapshot()
	require.NoError(t, err)
	require.NotNil(t, accountsConfig.VaultSetup)
	require.Len(t, accountsConfig.VaultSetup.Keys, 3)
}

// Only vault accounts have a descriptor to export, and nothing is written if the user does not pick
// a file.
func TestExportVaultDescriptor(t *testing.T) {
	setup := newVaultTestSetup(t)
	b := setup.backend
	b.registerKeystore(setup.keystores[0])
	accountCode, err := b.AddVault(setup.descriptor, "My vault", setup.keystores[0])
	require.NoError(t, err)

	require.Equal(t, errp.ErrUserAbort, errp.Cause(b.ExportVaultDescriptor(accountCode)))
	require.Error(t, b.ExportVaultDescriptor("unknown"))
	for _, view := range b.Accounts() {
		if !view.Record.IsMultisig() {
			require.Error(t, b.ExportVaultDescriptor(view.Record.Code))
			break
		}
	}
}
