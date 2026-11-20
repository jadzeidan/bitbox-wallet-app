// SPDX-License-Identifier: Apache-2.0

package signing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/stretchr/testify/require"
)

// Account-level xpubs at m/48'/0'/0'/2' of three unrelated keystores.
const (
	multisigTestXpub1 = "xpub6FEZ9Bv73h1vnE4TJG4QFj2RPXJhhsPbnXgFyH3ErLvpcZrDcynY65bhWga8PazWHLSLi23PoBhGcLcYW6JRiJ12zXZ9Aop4LbAqsS3gtcy"
	multisigTestXpub2 = "xpub6EGAio99SxruuNxoBtG4fbYx3xM8fs7wjYJLRNcUg7UQin3LTANQiUYyb3RLjZ2EAyLsQBrtbNENUGh3oWzjHtgfQ3mtjPNFgNMronzTTVR"
	multisigTestXpub3 = "xpub6EMfjyGVUvwhpc3WKN1zXhMFGKJGMaSBPqbja4tbGoYvRBSXeTBCaqrRDjcuGTcaY95JrrAnQvDG3pdQPdtnYUCugjeksHSbyZT7rq38VQF"
)

func mustParseXPub(t *testing.T, xpub string) *hdkeychain.ExtendedKey {
	t.Helper()
	key, err := hdkeychain.NewKeyFromString(xpub)
	require.NoError(t, err)
	return key
}

func multisigTestKeys(t *testing.T) []KeyInfo {
	t.Helper()
	keypath := mustKeypath("m/48'/0'/0'/2'")
	return []KeyInfo{
		{
			RootFingerprint:   []byte{0x11, 0x11, 0x11, 0x11},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: mustParseXPub(t, multisigTestXpub1),
		},
		{
			RootFingerprint:   []byte{0x22, 0x22, 0x22, 0x22},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: mustParseXPub(t, multisigTestXpub2),
		},
		{
			RootFingerprint:   []byte{0x33, 0x33, 0x33, 0x33},
			AbsoluteKeypath:   keypath,
			ExtendedPublicKey: mustParseXPub(t, multisigTestXpub3),
		},
	}
}

// A 2-of-3 configuration is accepted, and thresholds, key counts and keys outside of what a
// multisig script allows are rejected.
func TestBitcoinMultisigValidate(t *testing.T) {
	keys := multisigTestKeys(t)

	_, err := NewBitcoinMultisigConfiguration(2, keys)
	require.NoError(t, err)
	_, err = NewBitcoinMultisigConfiguration(1, keys[:2])
	require.NoError(t, err)

	_, err = NewBitcoinMultisigConfiguration(0, keys)
	require.Error(t, err)
	_, err = NewBitcoinMultisigConfiguration(4, keys)
	require.Error(t, err)
	_, err = NewBitcoinMultisigConfiguration(1, keys[:1])
	require.Error(t, err)

	tooManyKeys := make([]KeyInfo, multisigMaxKeys+1)
	for index := range tooManyKeys {
		tooManyKeys[index] = keys[0]
	}
	_, err = NewBitcoinMultisigConfiguration(2, tooManyKeys)
	require.Error(t, err)

	// The same key twice is rejected, even if its metadata and version bytes differ.
	duplicate := multisigTestKeys(t)
	duplicate[2].ExtendedPublicKey = mustParseXPub(t, multisigTestXpub1)
	duplicate[2].ExtendedPublicKey.SetNet(&chaincfg.TestNet3Params)
	_, err = NewBitcoinMultisigConfiguration(2, duplicate)
	require.ErrorContains(t, err, "more than once")

	invalidFingerprint := multisigTestKeys(t)
	invalidFingerprint[1].RootFingerprint = []byte{1, 2, 3}
	_, err = NewBitcoinMultisigConfiguration(2, invalidFingerprint)
	require.ErrorContains(t, err, "root fingerprint")

	missingKey := multisigTestKeys(t)
	missingKey[1].ExtendedPublicKey = nil
	_, err = NewBitcoinMultisigConfiguration(2, missingKey)
	require.ErrorContains(t, err, "extended public key is nil")

	privateKey := multisigTestKeys(t)
	xprv, err := hdkeychain.NewMaster(make([]byte, 32), &chaincfg.MainNetParams)
	require.NoError(t, err)
	privateKey[1].ExtendedPublicKey = xprv
	_, err = NewBitcoinMultisigConfiguration(2, privateKey)
	require.ErrorContains(t, err, "must be public")
}

// The descriptor lists the threshold and the keys in configuration order, with key origins and
// network-specific extended keys, and parsing it restores the same configuration.
func TestBitcoinMultisigDescriptorRoundtrip(t *testing.T) {
	cfg, err := NewBitcoinMultisigConfiguration(2, multisigTestKeys(t))
	require.NoError(t, err)
	multisig := cfg.BitcoinMultisig

	descriptor, err := multisig.Descriptor(&chaincfg.MainNetParams)
	require.NoError(t, err)
	payload, checksum, found := strings.Cut(descriptor, "#")
	require.True(t, found)
	require.Len(t, checksum, 8)
	require.Equal(t,
		"wsh(sortedmulti(2,"+
			"[11111111/48'/0'/0'/2']"+multisigTestXpub1+"/<0;1>/*,"+
			"[22222222/48'/0'/0'/2']"+multisigTestXpub2+"/<0;1>/*,"+
			"[33333333/48'/0'/0'/2']"+multisigTestXpub3+"/<0;1>/*))",
		payload)

	parsed, err := ParseBitcoinMultisigDescriptor(descriptor, &chaincfg.MainNetParams)
	require.NoError(t, err)
	require.Equal(t, multisig.Threshold, parsed.Threshold)
	require.Len(t, parsed.Keys, len(multisig.Keys))
	for index, key := range multisig.Keys {
		require.Equal(t, key.RootFingerprint, parsed.Keys[index].RootFingerprint)
		require.Equal(t,
			key.AbsoluteKeypath.Encode(), parsed.Keys[index].AbsoluteKeypath.Encode())
		require.Equal(t,
			key.ExtendedPublicKey.String(), parsed.Keys[index].ExtendedPublicKey.String())
	}

	// Testnet descriptors encode the keys as tpub. Parsing stores them with the mainnet version
	// bytes, like all extended keys in signing configurations.
	testnetDescriptor, err := multisig.Descriptor(&chaincfg.TestNet3Params)
	require.NoError(t, err)
	require.Contains(t, testnetDescriptor, "]tpub")
	require.NotContains(t, testnetDescriptor, "]xpub")
	parsedTestnet, err := ParseBitcoinMultisigDescriptor(testnetDescriptor, &chaincfg.TestNet3Params)
	require.NoError(t, err)
	require.Equal(t, multisigTestXpub1, parsedTestnet.Keys[0].ExtendedPublicKey.String())
}

// Descriptors using h or H as hardened marker are accepted, as exported by other wallets.
func TestParseBitcoinMultisigDescriptorHardenedMarkers(t *testing.T) {
	for _, marker := range []string{"h", "H"} {
		payload := "wsh(sortedmulti(2," +
			"[11111111/48" + marker + "/0" + marker + "/0" + marker + "/2" + marker + "]" + multisigTestXpub1 + "/<0;1>/*," +
			"[22222222/48" + marker + "/0" + marker + "/0" + marker + "/2" + marker + "]" + multisigTestXpub2 + "/<0;1>/*," +
			"[33333333/48" + marker + "/0" + marker + "/0" + marker + "/2" + marker + "]" + multisigTestXpub3 + "/<0;1>/*))"
		descriptor, err := addDescriptorChecksum(payload)
		require.NoError(t, err)
		parsed, err := ParseBitcoinMultisigDescriptor(descriptor, &chaincfg.MainNetParams)
		require.NoError(t, err)
		require.Equal(t, "m/48'/0'/0'/2'", parsed.Keys[2].AbsoluteKeypath.Encode())
	}
}

// Descriptors that are damaged, for another network, or describe anything other than a
// wsh(sortedmulti()) wallet with ranged receive and change keys are rejected.
func TestParseBitcoinMultisigDescriptorInvalid(t *testing.T) {
	cfg, err := NewBitcoinMultisigConfiguration(2, multisigTestKeys(t))
	require.NoError(t, err)
	descriptor, err := cfg.BitcoinMultisig.Descriptor(&chaincfg.MainNetParams)
	require.NoError(t, err)
	payload, _, _ := strings.Cut(descriptor, "#")

	withChecksum := func(payload string) string {
		result, err := addDescriptorChecksum(payload)
		require.NoError(t, err)
		return result
	}
	key1 := "[11111111/48'/0'/0'/2']" + multisigTestXpub1 + "/<0;1>/*"
	key2 := "[22222222/48'/0'/0'/2']" + multisigTestXpub2 + "/<0;1>/*"

	tests := []struct {
		name          string
		descriptor    string
		net           *chaincfg.Params
		expectedError string
	}{
		{
			name:          "missing checksum",
			descriptor:    payload,
			net:           &chaincfg.MainNetParams,
			expectedError: "checksum is missing",
		},
		{
			name:          "wrong checksum",
			descriptor:    payload + "#aaaaaaaa",
			net:           &chaincfg.MainNetParams,
			expectedError: "checksum does not match",
		},
		{
			name:          "modified payload",
			descriptor:    strings.Replace(descriptor, "sortedmulti(2", "sortedmulti(1", 1),
			net:           &chaincfg.MainNetParams,
			expectedError: "checksum does not match",
		},
		{
			name:          "different network",
			descriptor:    descriptor,
			net:           &chaincfg.TestNet3Params,
			expectedError: "different network",
		},
		{
			name:          "unsorted multi",
			descriptor:    withChecksum("wsh(multi(2," + key1 + "," + key2 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "only wsh(sortedmulti(...))",
		},
		{
			name:          "wrapped segwit",
			descriptor:    withChecksum("sh(wsh(sortedmulti(2," + key1 + "," + key2 + ")))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "only wsh(sortedmulti(...))",
		},
		{
			name:          "no keys",
			descriptor:    withChecksum("wsh(sortedmulti(2))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "number of keys",
		},
		{
			name:          "threshold too high",
			descriptor:    withChecksum("wsh(sortedmulti(3," + key1 + "," + key2 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "threshold",
		},
		{
			name:          "non-canonical threshold",
			descriptor:    withChecksum("wsh(sortedmulti(02," + key1 + "," + key2 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "invalid threshold",
		},
		{
			name: "receive chain only",
			descriptor: withChecksum(
				"wsh(sortedmulti(2," + strings.Replace(key1, "<0;1>", "0", 1) + "," + key2 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "unsupported key expression",
		},
		{
			name: "missing key origin",
			descriptor: withChecksum(
				"wsh(sortedmulti(2," + multisigTestXpub1 + "/<0;1>/*," + key2 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "unsupported key expression",
		},
		{
			name:          "duplicate key",
			descriptor:    withChecksum("wsh(sortedmulti(2," + key1 + "," + key1 + "))"),
			net:           &chaincfg.MainNetParams,
			expectedError: "more than once",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseBitcoinMultisigDescriptor(test.descriptor, test.net)
			require.ErrorContains(t, err, test.expectedError)
		})
	}
}

// A multisig configuration survives being persisted as JSON.
func TestBitcoinMultisigEncodeDecode(t *testing.T) {
	cfg, err := NewBitcoinMultisigConfiguration(2, multisigTestKeys(t))
	require.NoError(t, err)

	jsonBytes, err := json.Marshal(cfg)
	require.NoError(t, err)
	var decoded Configuration
	require.NoError(t, json.Unmarshal(jsonBytes, &decoded))
	require.Nil(t, decoded.BitcoinSimple)
	require.Nil(t, decoded.EthereumSimple)
	require.NotNil(t, decoded.BitcoinMultisig)
	require.NoError(t, decoded.BitcoinMultisig.Validate())

	expectedDescriptor, err := cfg.BitcoinMultisig.Descriptor(&chaincfg.MainNetParams)
	require.NoError(t, err)
	decodedDescriptor, err := decoded.BitcoinMultisig.Descriptor(&chaincfg.MainNetParams)
	require.NoError(t, err)
	require.Equal(t, expectedDescriptor, decodedDescriptor)
}

// The generic configuration accessors treat a multisig configuration as belonging to every
// keystore that holds one of its keys, and never as a numbered single-signature account.
func TestBitcoinMultisigConfigurationAccessors(t *testing.T) {
	cfg, err := NewBitcoinMultisigConfiguration(2, multisigTestKeys(t))
	require.NoError(t, err)
	configs := Configurations{cfg}

	require.True(t, cfg.IsMultisig())
	require.Equal(t, ScriptTypeP2WSH, cfg.ScriptType())
	require.Equal(t, "bitcoinMultisig;threshold=2;keys=11111111,22222222,33333333", cfg.String())
	require.Same(t, cfg.BitcoinMultisig, configs.Multisig())

	_, err = cfg.AccountNumber()
	require.Error(t, err)
	_, err = configs.AccountNumber()
	require.Error(t, err)
	_, err = configs.RootFingerprint()
	require.Error(t, err)

	require.Equal(t,
		[][]byte{{0x11, 0x11, 0x11, 0x11}, {0x22, 0x22, 0x22, 0x22}, {0x33, 0x33, 0x33, 0x33}},
		configs.RootFingerprints())
	require.True(t, configs.ContainsRootFingerprint([]byte{0x22, 0x22, 0x22, 0x22}))
	require.False(t, configs.ContainsRootFingerprint([]byte{0x44, 0x44, 0x44, 0x44}))
	require.Equal(t, 2, cfg.BitcoinMultisig.KeyIndex([]byte{0x33, 0x33, 0x33, 0x33}))
	require.Equal(t, -1, cfg.BitcoinMultisig.KeyIndex([]byte{0x44, 0x44, 0x44, 0x44}))
	require.Equal(t, -1, configs.FindScriptType(ScriptTypeP2WSH))

	// Single-signature configurations are not multisig and have exactly one root fingerprint.
	simple := NewBitcoinConfiguration(
		ScriptTypeP2WPKH,
		[]byte{1, 2, 3, 4},
		mustKeypath("m/84'/0'/0'"),
		mustXPub(t, &chaincfg.MainNetParams),
	)
	simpleConfigs := Configurations{simple, simple}
	require.False(t, simple.IsMultisig())
	require.Nil(t, simpleConfigs.Multisig())
	require.Equal(t, [][]byte{{1, 2, 3, 4}}, simpleConfigs.RootFingerprints())
}
