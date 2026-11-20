// SPDX-License-Identifier: Apache-2.0

package btc_test

import (
	"os"
	"testing"
	"time"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain"
	blockchainMock "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain/mocks"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/maketx"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore/software"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/logging"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/socksproxy"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/test"
	"github.com/btcsuite/btcd/btcutil/v2"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/stretchr/testify/require"
)

const multisigTestFeePerKb = btcutil.Amount(5000)

// multisigTestEnv is a 2-of-3 multisig testnet account whose keys are held by three software
// keystores, which sign like hardware wallets would. The keystore used by the account can be
// swapped, like the user swaps devices.
type multisigTestEnv struct {
	account       *btc.Account
	coin          *btc.Coin
	configuration *signing.Configuration
	keystores     [3]*software.Keystore
	// connected is the keystore the account is used with.
	connected *software.Keystore
	// broadcast holds the transactions that were broadcast.
	broadcast []*wire.MsgTx
	// outputs maps the outpoints spent in the tests to the outputs they refer to.
	outputs map[wire.OutPoint]*wire.TxOut
}

func newMultisigTestEnv(t *testing.T) *multisigTestEnv {
	t.Helper()
	net := &chaincfg.TestNet3Params
	dbFolder := test.TstTempDir("btc-dbfolder")
	notesFolder := test.TstTempDir("btc-notesfolder")
	t.Cleanup(func() {
		_ = os.RemoveAll(dbFolder)
		_ = os.RemoveAll(notesFolder)
	})

	env := &multisigTestEnv{outputs: map[wire.OutPoint]*wire.TxOut{}}
	env.coin = btc.NewCoin(
		coin.CodeTBTC, "Bitcoin Testnet", "TBTC", coin.BtcUnitDefault, net, dbFolder, nil, "", "",
		socksproxy.NewSocksProxy(false, ""))
	chain := &blockchainMock.BlockchainMock{}
	chain.MockRegisterOnConnectionErrorChangedEvent = func(func(error)) {}
	chain.MockTransactionBroadcast = func(tx *wire.MsgTx) error {
		env.broadcast = append(env.broadcast, tx)
		return nil
	}
	env.coin.TstSetMakeBlockchain(func() blockchain.Interface { return chain })

	keypath, err := signing.NewAbsoluteKeypath("m/48'/1'/0'/2'")
	require.NoError(t, err)
	keys := make([]signing.KeyInfo, 3)
	for index, pin := range []string{"first", "second", "third"} {
		ks := software.NewKeystoreFromPINWithEdition(pin, software.EditionMulti)
		env.keystores[index] = ks
		rootFingerprint, err := ks.RootFingerprint()
		require.NoError(t, err)
		xpub, err := ks.ExtendedPublicKey(env.coin, keypath)
		require.NoError(t, err)
		keys[index] = signing.KeyInfo{
			RootFingerprint:   rootFingerprint,
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: xpub,
		}
	}
	env.configuration, err = signing.NewBitcoinMultisigConfiguration(2, keys)
	require.NoError(t, err)
	// Like after creating a vault, the account is registered on every keystore.
	for _, ks := range env.keystores {
		require.NoError(t, ks.RegisterBTCMultisig(env.coin, env.configuration.BitcoinMultisig, "Vault"))
	}
	env.connected = env.keystores[0]

	env.account = btc.NewAccount(
		&accounts.AccountConfig{
			Code:                  "vaultcode",
			SigningConfigurations: signing.Configurations{env.configuration},
			DBFolder:              dbFolder,
			NotesFolder:           notesFolder,
			GetNotifier:           func(signing.Configurations) accounts.Notifier { return nil },
			GetSaveFilename:       func(suggestedFilename string) string { return suggestedFilename },
			ConnectKeystore:       func() (keystore.Keystore, error) { return env.connected, nil },
		},
		env.coin, nil, nil,
		logging.Get().WithGroup("multisig_tx_test"),
		nil,
	)
	require.NoError(t, env.account.Initialize())
	t.Cleanup(env.account.Close)
	require.Eventually(t, env.account.Synced, time.Second, 10*time.Millisecond)
	return env
}

func (env *multisigTestEnv) address(change bool, index uint32) *addresses.AccountAddress {
	return addresses.NewAccountAddress(
		env.configuration,
		types.Derivation{Change: change, AddressIndex: index},
		env.coin.Net(),
		logging.Get().WithGroup("multisig_tx_test"),
	)
}

// propose makes the account propose a transaction that spends the given amounts, held by the
// first receive addresses of the account, paying `amount` to an external address.
func (env *multisigTestEnv) propose(t *testing.T, amount int64, inputAmounts ...int64) *maketx.TxProposal {
	t.Helper()
	utxos := map[wire.OutPoint]maketx.UTXO{}
	for index, inputAmount := range inputAmounts {
		address := env.address(false, uint32(index))
		outPoint := wire.OutPoint{Hash: chainhash.HashH([]byte{byte(index)}), Index: uint32(index)}
		txOut := wire.NewTxOut(inputAmount, address.PubkeyScript())
		utxos[outPoint] = maketx.UTXO{TxOut: txOut, Address: address}
		env.outputs[outPoint] = txOut
	}
	recipient, err := env.coin.AddressToPkScript("tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx")
	require.NoError(t, err)
	txProposal, err := maketx.NewTx(
		env.coin,
		utxos,
		maketx.NewOutputInfo(recipient),
		amount,
		multisigTestFeePerKb,
		env.address(true, 0),
		logging.Get().WithGroup("multisig_tx_test"),
	)
	require.NoError(t, err)
	env.account.TstSetActiveTxProposal(txProposal)
	return txProposal
}

// requireValid checks that the transaction spends the outputs with valid witnesses.
func (env *multisigTestEnv) requireValid(t *testing.T, tx *wire.MsgTx) {
	t.Helper()
	fetcher := txscript.NewMultiPrevOutFetcher(env.outputs)
	sigHashes := txscript.NewTxSigHashes(tx, fetcher)
	for index, txIn := range tx.TxIn {
		spent := env.outputs[txIn.PreviousOutPoint]
		require.NotNil(t, spent)
		engine, err := txscript.NewEngine(
			spent.PkScript, tx, index, txscript.StandardVerifyFlags, nil, sigHashes, spent.Value, fetcher)
		require.NoError(t, err)
		require.NoError(t, engine.Execute())
	}
}

func fingerprint(t *testing.T, ks *software.Keystore) []byte {
	t.Helper()
	rootFingerprint, err := ks.RootFingerprint()
	require.NoError(t, err)
	return rootFingerprint
}

// A transaction from a multisig account is signed by one keystore, kept pending, signed by a
// second keystore and then broadcast. The broadcast transaction is valid, pays the proposed
// amount, and its fee covers its actual size.
func TestMultisigTxTwoSigners(t *testing.T) {
	env := newMultisigTestEnv(t)
	account := env.account
	txProposal := env.propose(t, 450_000, 200_000, 300_000)
	require.Len(t, txProposal.Psbt.UnsignedTx.TxIn, 2)

	pending, err := account.PendingMultisigTx()
	require.NoError(t, err)
	require.Nil(t, pending)

	require.NoError(t, account.CreatePendingMultisigTx("rent"))
	require.Empty(t, env.broadcast)

	pending, err = account.PendingMultisigTx()
	require.NoError(t, err)
	require.NotNil(t, pending)
	require.Equal(t, "rent", pending.Note)
	require.Equal(t, 2, pending.Threshold)
	require.Equal(t, [][]byte{fingerprint(t, env.keystores[0])}, pending.SignedBy)
	require.Len(t, pending.Recipients, 1)
	require.Equal(t, "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx", pending.Recipients[0].Address)
	require.Equal(t, coin.NewAmountFromInt64(450_000), pending.Recipients[0].Amount)
	require.Equal(t, coin.NewAmountFromInt64(450_000), pending.Amount)
	require.Equal(t, coin.NewAmountFromInt64(int64(txProposal.Fee)), pending.Fee)
	require.Equal(t, coin.NewAmountFromInt64(450_000+int64(txProposal.Fee)), pending.Total)

	// A second transaction can't be proposed or created while one is pending.
	_, _, _, err = account.TxProposal(&accounts.TxProposalArgs{
		RecipientAddress: "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx",
	})
	require.Equal(t, btc.ErrMultisigTxPending, errp.Cause(err))
	require.Equal(t, btc.ErrMultisigTxPending, errp.Cause(account.CreatePendingMultisigTx("other")))

	// The same keystore can't provide the second signature.
	txID, err := account.SignPendingMultisigTx()
	require.Equal(t, btc.ErrMultisigAlreadySigned, errp.Cause(err))
	require.Empty(t, txID)
	require.Empty(t, env.broadcast)

	// The user swaps to another keystore of the account.
	env.connected = env.keystores[2]
	txID, err = account.SignPendingMultisigTx()
	require.NoError(t, err)
	require.Len(t, env.broadcast, 1)
	signedTx := env.broadcast[0]
	require.Equal(t, signedTx.TxID(), txID)
	env.requireValid(t, signedTx)

	// Each input is spent with an empty item, two signatures and the witness script.
	for _, txIn := range signedTx.TxIn {
		require.Len(t, txIn.Witness, 4)
		require.Empty(t, txIn.Witness[0])
		require.Len(t, txIn.Witness[3], 105)
	}
	var outputsSum int64
	paidToRecipient := false
	for _, txOut := range signedTx.TxOut {
		outputsSum += txOut.Value
		if txOut.Value == 450_000 {
			paidToRecipient = true
		}
	}
	require.True(t, paidToRecipient)
	fee := 500_000 - outputsSum
	require.Equal(t, int64(txProposal.Fee), fee)
	// The fee was estimated for the worst case size, so it pays at least the requested rate.
	weight := int64(signedTx.SerializeSizeStripped()*3 + signedTx.SerializeSize())
	vsize := (weight + 3) / 4
	require.GreaterOrEqual(t, fee*1000, vsize*int64(multisigTestFeePerKb))
	require.Less(t, fee*1000, (vsize+8)*int64(multisigTestFeePerKb))

	// The transaction is not pending anymore, and its note is kept.
	pending, err = account.PendingMultisigTx()
	require.NoError(t, err)
	require.Nil(t, pending)
	require.Equal(t, "rent", account.TxNote(signedTx.TxHash().String()))
	_, err = account.SignPendingMultisigTx()
	require.Equal(t, btc.ErrMultisigNoPendingTx, errp.Cause(err))
}

// A keystore only signs for a multisig account that is registered on it. The pending transaction
// can be discarded, which makes the account available for a new transaction.
func TestMultisigTxRegistrationAndDiscard(t *testing.T) {
	env := newMultisigTestEnv(t)
	account := env.account
	env.propose(t, 100_000, 400_000)

	// A keystore on which the account is not registered, like a device that was reset.
	unregistered := software.NewKeystoreFromPINWithEdition("first", software.EditionMulti)
	env.connected = unregistered
	require.Equal(t, btc.ErrMultisigNotRegistered, errp.Cause(account.CreatePendingMultisigTx("")))
	pending, err := account.PendingMultisigTx()
	require.NoError(t, err)
	require.Nil(t, pending)

	env.connected = env.keystores[1]
	require.NoError(t, account.CreatePendingMultisigTx(""))
	env.connected = unregistered
	_, err = account.SignPendingMultisigTx()
	require.Equal(t, btc.ErrMultisigNotRegistered, errp.Cause(err))
	pending, err = account.PendingMultisigTx()
	require.NoError(t, err)
	require.Equal(t, [][]byte{fingerprint(t, env.keystores[1])}, pending.SignedBy)

	require.NoError(t, account.DiscardPendingMultisigTx())
	pending, err = account.PendingMultisigTx()
	require.NoError(t, err)
	require.Nil(t, pending)
	require.NoError(t, account.DiscardPendingMultisigTx())
	require.Empty(t, env.broadcast)

	// A new transaction can be created after discarding.
	env.propose(t, 100_000, 400_000)
	env.connected = env.keystores[1]
	require.NoError(t, account.CreatePendingMultisigTx(""))
}

// If broadcasting fails, the fully signed transaction stays pending and is broadcast when trying
// again, without another signature.
func TestMultisigTxBroadcastRetry(t *testing.T) {
	env := newMultisigTestEnv(t)
	account := env.account
	env.propose(t, 100_000, 400_000)
	require.NoError(t, account.CreatePendingMultisigTx(""))

	chain := account.Coin().(*btc.Coin).Blockchain().(*blockchainMock.BlockchainMock)
	broadcastErr := errp.New("offline")
	chain.MockTransactionBroadcast = func(*wire.MsgTx) error { return broadcastErr }
	env.connected = env.keystores[1]
	_, err := account.SignPendingMultisigTx()
	require.Equal(t, broadcastErr, errp.Cause(err))
	pending, err := account.PendingMultisigTx()
	require.NoError(t, err)
	require.Len(t, pending.SignedBy, 2)

	// No keystore is asked to sign again: the third keystore is connected but does not sign.
	chain.MockTransactionBroadcast = func(tx *wire.MsgTx) error {
		env.broadcast = append(env.broadcast, tx)
		return nil
	}
	env.connected = env.keystores[2]
	txID, err := account.SignPendingMultisigTx()
	require.NoError(t, err)
	require.Len(t, env.broadcast, 1)
	require.Equal(t, env.broadcast[0].TxID(), txID)
	env.requireValid(t, env.broadcast[0])
	for _, txIn := range env.broadcast[0].TxIn {
		require.Len(t, txIn.Witness, 4)
	}
}
