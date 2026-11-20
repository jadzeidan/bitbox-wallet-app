// SPDX-License-Identifier: Apache-2.0

package addresses_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/addresses"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/logging"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/stretchr/testify/require"
)

func multisigConfiguration(t *testing.T, threshold int, xpubs []string) *signing.Configuration {
	t.Helper()
	keypath, err := signing.NewAbsoluteKeypath("m/48'/0'/0'/2'")
	require.NoError(t, err)
	keys := make([]signing.KeyInfo, len(xpubs))
	for index, xpub := range xpubs {
		extendedPublicKey, err := hdkeychain.NewKeyFromString(xpub)
		require.NoError(t, err)
		keys[index] = signing.KeyInfo{
			RootFingerprint:   []byte{0, 0, 0, byte(index + 1)},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: extendedPublicKey,
		}
	}
	configuration, err := signing.NewBitcoinMultisigConfiguration(threshold, keys)
	require.NoError(t, err)
	return configuration
}

// Multisig addresses match the addresses the BitBox02 derives and displays for the same
// configuration. The vectors are taken from the BitBox02 firmware unit tests (P2WSH cases of
// test_address_multisig), all at <change=1>/<address index=2>.
func TestMultisigAddressFirmwareVectors(t *testing.T) {
	tests := []struct {
		name            string
		net             *chaincfg.Params
		threshold       int
		xpubs           []string
		expectedAddress string
	}{
		{
			name:      "1-of-2 mainnet",
			net:       &chaincfg.MainNetParams,
			threshold: 1,
			xpubs: []string{
				"xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy",
				"xpub6EGAio99SxruuNxoBtG4fbYx3xM8fs7wjYJLRNcUg7UQin3LTANQiUYyb3RLjZ2EAyLsQBrtbNENUGh3oWzjHtgfQ3mtjPNFgNMronzTTVR",
			},
			expectedAddress: "bc1q2fhgukymf0caaqrhfxrdju4wm94wwrch2ukntl5fuc0faz8zm49q0h6ss8",
		},
		{
			name:      "1-of-2 testnet",
			net:       &chaincfg.TestNet3Params,
			threshold: 1,
			xpubs: []string{
				"xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF",
				"xpub6ERxBysTYfQyY4USv6c6J1HNVv9hpZFN9LHVPu47Ac4rK8fLy6NnAeeAHyEsMvG4G66ay5aFZii2VM7wT3KxLKX8Q8keZPd67kRGmrD1WJj",
			},
			expectedAddress: "tb1qw2scxk3zq0znr4ug9xkf3n7nfjsc8ldvemrm9dxjpl847zyu6afsfjjy28",
		},
		{
			name:      "7-of-15 testnet",
			net:       &chaincfg.TestNet3Params,
			threshold: 7,
			xpubs: []string{
				"xpub6Eu7xJRyXRCi4eLYhJPnfZVjgAQtM7qFaEZwUhvgxGf4enEZMxevGzWvZTawCj9USP2MFTEhKQAwnqHwoaPHetTLqGuvq5r5uaLKyGx5QDZ",
				"xpub6EQcxF2jFkYGn89AwoQJEEJkYMbRjED9AZgt7bkxQA5BLhZEoaQpUHcADbB5GxcMrTdDSGmjP7M3u462Q9otyE2PPam66P5KFLWitPVfYz9",
				"xpub6EP4EycVS5dq1PN7ZqsxBtptkYhfLvLGokZjnB3fvPshMiAohh6E5TaJjAafZWoPRjo6uiZxhtDXLgCuk81ooQgwrsnEdfSWSfa4VUtX8nu",
				"xpub6Eszd4BGGmHShcGtys5gbvV2zrBtW1gaorKf9YuvV4L3bePw7XePyyb2DKswZ5AhFfkcQwjQsiJEUTKhfRstRdHZUjQnJ2RJoQqL8g7FS4b",
				"xpub6Df3nbvH6P3FTvjgKaZcSuydyEofK545U4Bb15JY8R9MtFkKrhYrc3bpEF6fHtNM7xQ1qHwsVpS56TJWUjbKcmRwPkQr17ovV2RaVSJaBq3",
				"xpub6FQQ62gUYzS9wnHWHMPLWrpVnzS8xAf8XvfW1xzXEXTkTCtBrfbeww2zNeCgm3PbueMoq8opQvQDzp5Yf9EtiqVd7d1ASDoWSC1m7g1KHza",
				"xpub6EQNZUUAzJAoFAVVetYUrFVrf7mLyYsnHiQihkA3KPhoRHx7m6SgKBYV4z5Rd9CvUc11ACN8Ap5Wxigt6GYRPUqXGFfm3833ezJpjAmvJKt",
				"xpub6EGZy7cizYn2zUf9NT4qJ3Kr1ZrxdzPRcv2CwAnB1BTGWw7n9ZgDYvwmzzJXM6V7AgZ6CL3DrARZk5DzM9o8tz2RVTeC7QoHh9SxbW3b7Pw",
				"xpub6DaV7oCAkm4HJQMoProrrKYq1RvcgpStgYUCzLRaaeJSBSy9WBRFMNnQyAWJUYy9myUFRTvogq1C2f7x4A2yhtYgr7gL6eZXv2eJvzU12pe",
				"xpub6FFVRbdHt5DgHqR69KuWXRVDp93e1xKxv8rRLwhhCGnWaoF1ecnfdxpg2Nf1pvJTgT1UYg28CVt7YbUXFJL86vi9FaPN9QGtWLeCmf9dA24",
				"xpub6FNywxebMjvSSginZrk7DfNmAHvPJAy3j6pJ9FmUQCoh4FKPzNymdHnkA1z77Ke4GK7g5GkdrBhpyXfWTbZkH6Yo1t4v524wDwF8SAKny9J",
				"xpub6F1V9y6gXejomurTy2hN1UDCJidYahVkqtQJSZLYmcPcPDWkxGgWTrrLnCrCkGESSUSq6GpVVQx9kejPV97BEa9F85utABNL9r6xyPZFiDm",
				"xpub6ECHc4kmTC2tQg2ZoAoazwyag9C4V6yFsZEhjwMJixdVNsUibot6uEvsZY38ZLVqWCtyc9gbzFEwHQLHCT8EiDDKSNNsFAB8NQYRgkiAQwu",
				"xpub6F7CaxXzBCtvXwpRi61KYyhBRkgT1856ujHV5AbJK6ySCUYoDruBH6Pnsi6eHkDiuKuAJ2tSc9x3emP7aax9Dc3u7nP7RCQXEjLKihQu6w1",
				"xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF",
			},
			expectedAddress: "tb1qndz49j0arp8g6jc8vcrgf9ugrsw96a0j5d7vqcun6jev6rlv47jsv99y5m",
		},
		{
			name:      "1-of-2 mainnet, second vector",
			net:       &chaincfg.MainNetParams,
			threshold: 1,
			xpubs: []string{
				"xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy",
				"xpub68yJakxtRe3azab9rb8DJqxDeCG7oBY3zhsNnvZybjTE9qc9Hgw4bCqdLjVGykZrwD6CC6r6xHrnuep5Dmb9uq2R4emCm8YzBuddFyhgvAD",
			},
			expectedAddress: "bc1qtsvlhzltl05etjjeqh00urwttu6ep4xn3c0ccndz77unttut9h0qvrcs04",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := addresses.NewAccountAddress(
				multisigConfiguration(t, test.threshold, test.xpubs),
				types.Derivation{Change: true, AddressIndex: 2},
				test.net,
				logging.Get().WithGroup("addresses_test"),
			)
			require.Equal(t, test.expectedAddress, address.EncodeAddress())
			require.True(t, address.IsForNet(test.net))
		})
	}
}

// A 2-of-3 address is a P2WSH output of the script `2 <three sorted public keys> 3
// OP_CHECKMULTISIG`. The order of the keys in the configuration does not change the address, and
// receive and change addresses differ.
func TestMultisigAddressScript(t *testing.T) {
	xpubs := []string{
		"xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy",
		"xpub6EGAio99SxruuNxoBtG4fbYx3xM8fs7wjYJLRNcUg7UQin3LTANQiUYyb3RLjZ2EAyLsQBrtbNENUGh3oWzjHtgfQ3mtjPNFgNMronzTTVR",
		"xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF",
	}
	log := logging.Get().WithGroup("addresses_test")
	net := &chaincfg.MainNetParams
	derivation := types.Derivation{Change: false, AddressIndex: 5}
	address := addresses.NewAccountAddress(multisigConfiguration(t, 2, xpubs), derivation, net, log)

	require.Nil(t, address.PublicKey)
	require.Nil(t, address.RedeemScript)
	require.True(t, txscript.IsPayToWitnessScriptHash(address.PubkeyScript()))

	tokenizer := txscript.MakeScriptTokenizer(0, address.WitnessScript)
	var opcodes []byte
	var publicKeys [][]byte
	for tokenizer.Next() {
		opcodes = append(opcodes, tokenizer.Opcode())
		if data := tokenizer.Data(); data != nil {
			publicKeys = append(publicKeys, data)
		}
	}
	require.NoError(t, tokenizer.Err())
	require.Equal(t,
		[]byte{
			txscript.OP_2,
			txscript.OP_DATA_33, txscript.OP_DATA_33, txscript.OP_DATA_33,
			txscript.OP_3,
			txscript.OP_CHECKMULTISIG,
		},
		opcodes)
	require.Len(t, publicKeys, 3)
	require.True(t, slices.IsSortedFunc(publicKeys, bytes.Compare))
	require.Len(t, address.WitnessScript, 105)

	reordered := []string{xpubs[2], xpubs[0], xpubs[1]}
	reorderedAddress := addresses.NewAccountAddress(
		multisigConfiguration(t, 2, reordered), derivation, net, log)
	require.Equal(t, address.EncodeAddress(), reorderedAddress.EncodeAddress())
	require.Equal(t, address.ID(), reorderedAddress.ID())

	changeAddress := addresses.NewAccountAddress(
		multisigConfiguration(t, 2, xpubs),
		types.Derivation{Change: true, AddressIndex: 5},
		net,
		log,
	)
	require.NotEqual(t, address.EncodeAddress(), changeAddress.EncodeAddress())

	otherThreshold := addresses.NewAccountAddress(multisigConfiguration(t, 3, xpubs), derivation, net, log)
	require.NotEqual(t, address.EncodeAddress(), otherThreshold.EncodeAddress())
}
