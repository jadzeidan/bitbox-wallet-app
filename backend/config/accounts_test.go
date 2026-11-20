// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/test"
	"github.com/stretchr/testify/require"
)

func TestLookup(t *testing.T) {
	cfg := AccountsConfig{
		Accounts: []*Account{
			{Code: "a"},
			{Code: "b"},
		},
	}
	acct := cfg.Lookup("a")
	require.NotNil(t, acct)
	require.Equal(t, "a", string(acct.Code))

	acct = cfg.Lookup("b")
	require.NotNil(t, acct)
	require.Equal(t, "b", string(acct.Code))

	require.Nil(t, cfg.Lookup("c"))

	acct = cfg.Lookup("a")
	require.NotNil(t, acct)
	acct.Name = "foo"
	require.Equal(t, "foo", cfg.Accounts[0].Name)
}

func TestLookupByXpub(t *testing.T) {
	keypath, err := signing.NewAbsoluteKeypath("m/84'/1'/0'")
	require.NoError(t, err)

	someXPub := "xpub661MyMwAqRbcGAo79WnM4653Aog3nKuTYq2Wy1iURzbaGE36dgnPPMxX5oCvCiky5bFbS2jS9RfAHVtpCNCEBwy6BUjLxhXYGu19NwTgrFX"
	someAccountCode := accountsTypes.Code("a")
	cfg := AccountsConfig{
		Accounts: []*Account{
			{
				Code: someAccountCode,
				SigningConfigurations: signing.Configurations{
					signing.NewBitcoinConfiguration(
						signing.ScriptTypeP2WPKH,
						[]byte{1, 2, 3, 4},
						keypath,
						test.TstMustXKey(someXPub),
					),
				},
			},
		},
	}

	_, err = cfg.LookupByXpub("foo")
	require.Error(t, err)

	acctCode, err := cfg.LookupByXpub(someXPub)
	require.NoError(t, err)
	require.Equal(t, someAccountCode, acctCode)
}

func TestSetTokenActive(t *testing.T) {
	// not an ETH account.
	require.Error(t, (&Account{CoinCode: coin.CodeSEPETH}).SetTokenActive("TOKEN", true))

	acct := &Account{
		CoinCode: coin.CodeETH,
	}
	require.NoError(t, acct.SetTokenActive("TOKEN-1", true))
	require.Equal(t, []string{"TOKEN-1"}, acct.ActiveTokens)
	require.NoError(t, acct.SetTokenActive("TOKEN-2", true))
	require.Equal(t, []string{"TOKEN-1", "TOKEN-2"}, acct.ActiveTokens)

	require.NoError(t, acct.SetTokenActive("TOKEN-1", false))
	require.Equal(t, []string{"TOKEN-2"}, acct.ActiveTokens)
}

func TestSetReceiveScriptType(t *testing.T) {
	keypath84, err := signing.NewAbsoluteKeypath("m/84'/0'/0'")
	require.NoError(t, err)
	keypath86, err := signing.NewAbsoluteKeypath("m/86'/0'/0'")
	require.NoError(t, err)

	acct := &Account{
		SigningConfigurations: signing.Configurations{
			signing.NewBitcoinConfiguration(
				signing.ScriptTypeP2WPKH,
				[]byte{1, 2, 3, 4},
				keypath84,
				test.TstMustXKey("xpub661MyMwAqRbcGAo79WnM4653Aog3nKuTYq2Wy1iURzbaGE36dgnPPMxX5oCvCiky5bFbS2jS9RfAHVtpCNCEBwy6BUjLxhXYGu19NwTgrFX"),
			),
			signing.NewBitcoinConfiguration(
				signing.ScriptTypeP2TR,
				[]byte{1, 2, 3, 4},
				keypath86,
				test.TstMustXKey("xpub661MyMwAqRbcGAo79WnM4653Aog3nKuTYq2Wy1iURzbaGE36dgnPPMxX5oCvCiky5bFbS2jS9RfAHVtpCNCEBwy6BUjLxhXYGu19NwTgrFX"),
			),
		},
	}

	require.NoError(t, acct.SetReceiveScriptType(signing.ScriptTypeP2TR))
	require.NotNil(t, acct.ReceiveScriptType)
	require.Equal(t, signing.ScriptTypeP2TR, *acct.ReceiveScriptType)

	require.Error(t, acct.SetReceiveScriptType(signing.ScriptTypeP2WPKHP2SH))
}

func TestGetOrAddKeystore(t *testing.T) {
	cfg := &AccountsConfig{}
	fp1 := []byte("aaaa")
	fp2 := []byte("bbbb")

	ks := cfg.GetOrAddKeystore(fp1)
	ks.Name = "ks1"

	require.Len(t, cfg.Keystores, 1)
	require.Equal(t, ks, cfg.Keystores[0])
	require.Equal(t, fp1, []byte(cfg.Keystores[0].RootFingerprint))
	require.Equal(t, "ks1", cfg.Keystores[0].Name)

	ks = cfg.GetOrAddKeystore(fp1)
	require.Len(t, cfg.Keystores, 1)
	require.Equal(t, "ks1", ks.Name)

	ks = cfg.GetOrAddKeystore(fp2)
	ks.Name = "ks2"

	require.Len(t, cfg.Keystores, 2)
	require.Equal(t, ks, cfg.Keystores[1])
	require.Equal(t, fp2, []byte(cfg.Keystores[1].RootFingerprint))
	require.Equal(t, "ks2", cfg.Keystores[1].Name)
}

func TestMigrateActiveToken(t *testing.T) {
	config := &Config{
		appConfigFilename: "appConfigFilename",
		appConfig:         NewDefaultAppConfig(),

		accountsConfigFilename: "accountsConfigFilename",
		accountsConfig:         newDefaultAccountsConfig(),
	}

	accountConf := config.accountsConfig

	acct := &Account{
		CoinCode: coin.CodeETH,
	}

	accountConf.Accounts = append(accountConf.Accounts, acct)

	require.Equal(t, []*Account{{
		CoinCode: coin.CodeETH,
	}}, accountConf.Accounts)

	require.NoError(t, acct.SetTokenActive("eth-erc20-sai0x89d", true))
	require.Equal(t, []string{"eth-erc20-sai0x89d"}, acct.ActiveTokens)
	require.NoError(t, acct.SetTokenActive("TOKEN-2", true))
	require.Equal(t, []string{"eth-erc20-sai0x89d", "TOKEN-2"}, acct.ActiveTokens)

	require.NoError(t, migrateActiveTokens(&accountConf))

	// eth-erc20-sai0x89d was removed by the migration.
	require.Equal(t, []string{"TOKEN-2"}, acct.ActiveTokens)
}

func multisigTestAccount(t *testing.T, code accountsTypes.Code) *Account {
	t.Helper()
	keypath, err := signing.NewAbsoluteKeypath("m/48'/0'/0'/2'")
	require.NoError(t, err)
	keys := make([]signing.KeyInfo, 3)
	for index, xpub := range []string{
		"xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy",
		"xpub6EGAio99SxruuNxoBtG4fbYx3xM8fs7wjYJLRNcUg7UQin3LTANQiUYyb3RLjZ2EAyLsQBrtbNENUGh3oWzjHtgfQ3mtjPNFgNMronzTTVR",
		"xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF",
	} {
		keys[index] = signing.KeyInfo{
			RootFingerprint:   []byte{byte(index + 1), 0, 0, 0},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: test.TstMustXKey(xpub),
		}
	}
	configuration, err := signing.NewBitcoinMultisigConfiguration(2, keys)
	require.NoError(t, err)
	return &Account{
		Code:                  code,
		SigningConfigurations: signing.Configurations{configuration},
	}
}

// A multisig account is never found by the xpub of one of its keys, and looking up an xpub works
// with multisig accounts present.
func TestLookupByXpubMultisig(t *testing.T) {
	keypath, err := signing.NewAbsoluteKeypath("m/84'/0'/0'")
	require.NoError(t, err)
	someXPub := "xpub661MyMwAqRbcGAo79WnM4653Aog3nKuTYq2Wy1iURzbaGE36dgnPPMxX5oCvCiky5bFbS2jS9RfAHVtpCNCEBwy6BUjLxhXYGu19NwTgrFX"
	multisigAccount := multisigTestAccount(t, "vault")
	cfg := AccountsConfig{
		Accounts: []*Account{
			multisigAccount,
			{
				Code: "a",
				SigningConfigurations: signing.Configurations{
					signing.NewBitcoinConfiguration(
						signing.ScriptTypeP2WPKH,
						[]byte{1, 2, 3, 4},
						keypath,
						test.TstMustXKey(someXPub),
					),
				},
			},
		},
	}
	require.True(t, multisigAccount.IsMultisig())
	require.False(t, cfg.Accounts[1].IsMultisig())

	acctCode, err := cfg.LookupByXpub(someXPub)
	require.NoError(t, err)
	require.Equal(t, accountsTypes.Code("a"), acctCode)

	multisigKey := multisigAccount.SigningConfigurations[0].BitcoinMultisig.Keys[0]
	_, err = cfg.LookupByXpub(multisigKey.ExtendedPublicKey.String())
	require.Error(t, err)
}

// A multisig account is watch-only as soon as one of the keystores holding its keys is marked as
// watchonly.
func TestIsAccountWatchOnlyMultisig(t *testing.T) {
	account := multisigTestAccount(t, "vault")
	cfg := AccountsConfig{
		Accounts: []*Account{account},
		Keystores: []*Keystore{
			{RootFingerprint: []byte{1, 0, 0, 0}},
			{RootFingerprint: []byte{3, 0, 0, 0}},
			{RootFingerprint: []byte{9, 9, 9, 9}, Watchonly: true},
		},
	}
	watchonly, err := cfg.IsAccountWatchOnly(account)
	require.NoError(t, err)
	require.False(t, watchonly)

	cfg.Keystores[1].Watchonly = true
	watchonly, err = cfg.IsAccountWatchOnly(account)
	require.NoError(t, err)
	require.True(t, watchonly)

	account.HiddenBecauseUnused = true
	watchonly, err = cfg.IsAccountWatchOnly(account)
	require.NoError(t, err)
	require.False(t, watchonly)

	_, err = cfg.IsAccountWatchOnly(&Account{Code: "empty"})
	require.Error(t, err)
}
