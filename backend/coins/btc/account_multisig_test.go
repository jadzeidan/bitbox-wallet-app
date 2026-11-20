// SPDX-License-Identifier: Apache-2.0

package btc

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/errors"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain"
	blockchainMock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain/mocks"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/maketx"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	keystoremock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore/mocks"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/paymentrequest"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/logging"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/socksproxy"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/test"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/stretchr/testify/require"
)

// mockMultisigAccount creates a 2-of-3 multisig testnet account that uses the given keystore.
func mockMultisigAccount(t *testing.T, ks keystore.Keystore) (*Account, *signing.Configuration) {
	t.Helper()
	net := &chaincfg.TestNet3Params
	dbFolder := test.TstTempDir("btc-dbfolder")
	t.Cleanup(func() { _ = os.RemoveAll(dbFolder) })

	btcCoin := NewCoin(
		coin.CodeTBTC, "Bitcoin Testnet", "TBTC", coin.BtcUnitDefault, net, dbFolder, nil, explorer, "",
		socksproxy.NewSocksProxy(false, ""))
	blockchainMock := &blockchainMock.BlockchainMock{}
	blockchainMock.MockRegisterOnConnectionErrorChangedEvent = func(f func(error)) {}
	btcCoin.TstSetMakeBlockchain(func() blockchain.Interface { return blockchainMock })

	keypath, err := signing.NewAbsoluteKeypath("m/48'/1'/0'/2'")
	require.NoError(t, err)
	keys := make([]signing.KeyInfo, 3)
	for index, xpub := range []string{
		"xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy",
		"xpub6EGAio99SxruuNxoBtG4fbYx3xM8fs7wjYJLRNcUg7UQin3LTANQiUYyb3RLjZ2EAyLsQBrtbNENUGh3oWzjHtgfQ3mtjPNFgNMronzTTVR",
		"xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF",
	} {
		extendedPublicKey, err := hdkeychain.NewKeyFromString(xpub)
		require.NoError(t, err)
		keys[index] = signing.KeyInfo{
			RootFingerprint:   []byte{0, 0, 0, byte(index + 1)},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: extendedPublicKey,
		}
	}
	configuration, err := signing.NewBitcoinMultisigConfiguration(2, keys)
	require.NoError(t, err)

	account := NewAccount(
		&accounts.AccountConfig{
			Code:                  "vaultcode",
			SigningConfigurations: signing.Configurations{configuration},
			DBFolder:              dbFolder,
			RateUpdater:           nil,
			GetNotifier:           func(signing.Configurations) accounts.Notifier { return nil },
			GetSaveFilename:       func(suggestedFilename string) string { return suggestedFilename },
			ConnectKeystore:       func() (keystore.Keystore, error) { return ks, nil },
		},
		btcCoin, nil, nil,
		logging.Get().WithGroup("account_test"),
		nil,
	)
	require.NoError(t, account.Initialize())
	t.Cleanup(account.Close)
	require.Eventually(t, account.Synced, time.Second, time.Millisecond*200)
	return account, configuration
}

// A multisig account offers P2WSH receive addresses derived from all of its keys, and reports its
// keys in the format the BitBox02 displays when registering the account.
func TestMultisigAccountReceive(t *testing.T) {
	account, configuration := mockMultisigAccount(t, &keystoremock.KeystoreMock{})

	addressLists, err := account.GetUnusedReceiveAddresses()
	require.NoError(t, err)
	require.Len(t, addressLists, 1)
	require.Equal(t, signing.ScriptTypeP2WSH, *addressLists[0].ScriptType)
	require.Len(t, addressLists[0].Addresses, receiveAddressesLimit)
	for index, address := range addressLists[0].Addresses {
		expected := addresses.NewAccountAddress(
			configuration,
			types.Derivation{Change: false, AddressIndex: uint32(index)},
			account.coin.Net(),
			account.log,
		)
		require.Equal(t, expected.EncodeAddress(), address.EncodeForHumans())
		require.True(t, strings.HasPrefix(address.EncodeForHumans(), "tb1q"))
		require.Len(t, address.EncodeForHumans(), 62)
	}

	info := account.Info()
	require.Len(t, info.SigningConfigurations, 1)
	multisig := info.SigningConfigurations[0].BitcoinMultisig
	require.NotNil(t, multisig)
	require.Equal(t, 2, multisig.Threshold)
	require.Len(t, multisig.Keys, 3)
	for index, key := range multisig.Keys {
		require.True(t, strings.HasPrefix(key.ExtendedPublicKey.String(), "Vpub"))
		require.Equal(t,
			configuration.BitcoinMultisig.Keys[index].RootFingerprint, key.RootFingerprint)
	}
	// The account's own configuration keeps the internal key encoding.
	require.True(t, strings.HasPrefix(
		configuration.BitcoinMultisig.Keys[0].ExtendedPublicKey.String(), "xpub"))
}

// An address of a multisig account is only shown on a keystore the account is registered on.
// Otherwise the caller learns that the account needs to be registered first.
func TestMultisigAccountVerifyAddress(t *testing.T) {
	registered := false
	ks := &keystoremock.KeystoreMock{
		CanVerifyAddressFunc: func(coin.Coin) (bool, bool, error) { return true, false, nil },
		BTCMultisigRegisteredFunc: func(coin.Coin, *signing.BitcoinMultisig) (bool, error) {
			return registered, nil
		},
		VerifyAddressBTCFunc: func(*signing.Configuration, types.Derivation, coin.Coin) error {
			return nil
		},
	}
	account, configuration := mockMultisigAccount(t, ks)

	addressLists, err := account.GetUnusedReceiveAddresses()
	require.NoError(t, err)
	address := addressLists[0].Addresses[3]

	verified, err := account.VerifyAddress(address.ID())
	require.Equal(t, ErrMultisigNotRegistered, errp.Cause(err))
	require.False(t, verified)
	require.Empty(t, ks.VerifyAddressBTCCalls())

	registered = true
	verified, err = account.VerifyAddress(address.ID())
	require.NoError(t, err)
	require.True(t, verified)
	calls := ks.VerifyAddressBTCCalls()
	require.Len(t, calls, 1)
	require.Same(t, configuration, calls[0].AccountConfiguration)
	require.Equal(t, types.Derivation{Change: false, AddressIndex: 3}, calls[0].Derivation)
}

// A multisig account can't be spent from in one step, pay to silent payment addresses or pay
// payment requests, and its addresses can't sign messages. Each is rejected with an error code.
func TestMultisigAccountUnsupportedOperations(t *testing.T) {
	account, _ := mockMultisigAccount(t, &keystoremock.KeystoreMock{
		CanSignMessageFunc: func(coin.Code) bool { return true },
	})

	// Sending needs the signatures of several keystores, see CreatePendingMultisigTx().
	_, err := account.SendTx("")
	require.Error(t, err)
	account.activeTxProposal = &maketx.TxProposal{}
	_, err = account.SendTx("")
	require.Equal(t, ErrMultisigSpendUnsupported, errp.Cause(err))

	_, _, _, err = account.TxProposal(&accounts.TxProposalArgs{
		RecipientAddress: "tsp1qqgste7k9hx0qftg6qmwlkqtwuy6cycyavzmzj85c6qdfhjdpdjtdgqjuexzk6murw56suy3e0rd2cgqvycxttddwsvgxe2usfpxumr70xc9pkqwv",
	})
	require.Equal(t, errors.ErrInvalidAddress, errp.Cause(err))

	_, _, _, err = account.TxProposal(&accounts.TxProposalArgs{
		RecipientAddress: "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx",
		PaymentRequest:   &paymentrequest.Request{},
	})
	require.Equal(t, ErrMultisigSpendUnsupported, errp.Cause(err))

	addressLists, err := account.GetUnusedReceiveAddresses()
	require.NoError(t, err)
	_, _, err = account.SignBTCMessageForAddress(addressLists[0].Addresses[0].ID(), "message")
	require.Equal(t, ErrMultisigSignMessageUnsupported, errp.Cause(err))

	_, _, err = SignBTCMessageUnusedAddress(account, "message", signing.ScriptTypeP2WPKH)
	require.Error(t, err)
}
