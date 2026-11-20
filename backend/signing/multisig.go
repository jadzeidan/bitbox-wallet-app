// SPDX-License-Identifier: Apache-2.0

package signing

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
)

const (
	// multisigMinKeys is the minimum number of cosigners of a multisig configuration.
	multisigMinKeys = 2
	// multisigMaxKeys is the maximum number of cosigners of a multisig configuration. It matches
	// the limit of the BitBox02.
	multisigMaxKeys = 15

	multisigDescriptorPrefix = "wsh(sortedmulti("
	multisigDescriptorSuffix = "))"
)

// multisigDescriptorKeyRegexp matches a key expression of the form
// [fingerprint/path]xpub/<0;1>/*. The hardened marker of a path element can be ', h or H.
var multisigDescriptorKeyRegexp = regexp.MustCompile(
	`^\[([0-9a-fA-F]{8})((?:/[0-9]+['hH]?)*)\]([1-9A-HJ-NP-Za-km-z]+)/<0;1>/\*$`)

// BitcoinMultisig represents a k-of-n Bitcoin multisig signing configuration. Outputs are P2WSH
// outputs whose witness script is `k <pubkeys sorted lexicographically> n OP_CHECKMULTISIG`, which
// corresponds to the descriptor wsh(sortedmulti(k,...)).
type BitcoinMultisig struct {
	// Threshold is the number of signatures required to spend.
	Threshold int `json:"threshold"`
	// Keys holds the account-level key of every cosigner. The public keys of an address are derived
	// from these at <change>/<address index>.
	Keys []KeyInfo `json:"keys"`
}

// NewBitcoinMultisigConfiguration creates a new multisig configuration. An error is returned if the
// threshold or the keys are invalid.
func NewBitcoinMultisigConfiguration(threshold int, keys []KeyInfo) (*Configuration, error) {
	multisig := &BitcoinMultisig{Threshold: threshold, Keys: keys}
	if err := multisig.Validate(); err != nil {
		return nil, err
	}
	return &Configuration{BitcoinMultisig: multisig}, nil
}

// SameExtendedPublicKey returns true if both keys derive the same children. The version bytes and
// the origin metadata (depth, parent fingerprint, child number) do not affect derivation.
func SameExtendedPublicKey(key1, key2 *hdkeychain.ExtendedKey) (bool, error) {
	publicKey1, err := key1.ECPubKey()
	if err != nil {
		return false, errp.WithStack(err)
	}
	publicKey2, err := key2.ECPubKey()
	if err != nil {
		return false, errp.WithStack(err)
	}
	return publicKey1.IsEqual(publicKey2) && bytes.Equal(key1.ChainCode(), key2.ChainCode()), nil
}

// Validate checks that the configuration is well-formed: the number of keys and the threshold are
// in range, every key is a public key with a root fingerprint, and no key appears twice.
func (multisig *BitcoinMultisig) Validate() error {
	if multisig == nil {
		return errp.New("multisig configuration is nil")
	}
	numKeys := len(multisig.Keys)
	if numKeys < multisigMinKeys || numKeys > multisigMaxKeys {
		return errp.Newf(
			"number of keys must be between %d and %d, got %d",
			multisigMinKeys, multisigMaxKeys, numKeys)
	}
	if multisig.Threshold < 1 || multisig.Threshold > numKeys {
		return errp.Newf(
			"threshold must be between 1 and %d, got %d", numKeys, multisig.Threshold)
	}
	for index, key := range multisig.Keys {
		if key.ExtendedPublicKey == nil {
			return errp.New("extended public key is nil")
		}
		if key.ExtendedPublicKey.IsPrivate() {
			return errp.New("extended key must be public")
		}
		if len(key.RootFingerprint) != 4 {
			return errp.Newf(
				"root fingerprint must be 4 bytes, got %d", len(key.RootFingerprint))
		}
		for _, otherKey := range multisig.Keys[:index] {
			same, err := SameExtendedPublicKey(key.ExtendedPublicKey, otherKey.ExtendedPublicKey)
			if err != nil {
				return err
			}
			if same {
				return errp.New("the same key appears more than once")
			}
		}
	}
	return nil
}

// RootFingerprints returns the root fingerprint of every key, in key order.
func (multisig *BitcoinMultisig) RootFingerprints() [][]byte {
	result := make([][]byte, len(multisig.Keys))
	for index, key := range multisig.Keys {
		result[index] = key.RootFingerprint
	}
	return result
}

// KeyIndex returns the index of the first key that belongs to the keystore with the given root
// fingerprint, or -1 if the keystore does not take part in this configuration.
func (multisig *BitcoinMultisig) KeyIndex(rootFingerprint []byte) int {
	for index, key := range multisig.Keys {
		if bytes.Equal(key.RootFingerprint, rootFingerprint) {
			return index
		}
	}
	return -1
}

// Descriptor returns a descriptor for this configuration in the form
// wsh(sortedmulti(k,[fingerprint/path]xpub-or-tpub/<0;1>/*,...))#checksum.
func (multisig *BitcoinMultisig) Descriptor(net *chaincfg.Params) (string, error) {
	if err := multisig.Validate(); err != nil {
		return "", err
	}
	if net == nil {
		return "", errp.New("network is nil")
	}
	args := make([]string, 0, len(multisig.Keys)+1)
	args = append(args, strconv.Itoa(multisig.Threshold))
	for _, key := range multisig.Keys {
		keyExpression, err := descriptorKeyExpression(key, net)
		if err != nil {
			return "", err
		}
		args = append(args, keyExpression)
	}
	return addDescriptorChecksum(
		multisigDescriptorPrefix + strings.Join(args, ",") + multisigDescriptorSuffix)
}

func (multisig *BitcoinMultisig) String() string {
	fingerprints := make([]string, len(multisig.Keys))
	for index, key := range multisig.Keys {
		fingerprints[index] = hex.EncodeToString(key.RootFingerprint)
	}
	return fmt.Sprintf(
		"threshold=%d;keys=%s", multisig.Threshold, strings.Join(fingerprints, ","))
}

// parseDescriptorKeyExpression parses a key expression of the form
// [fingerprint/path]xpub/<0;1>/*. The extended key must be encoded for the given network.
func parseDescriptorKeyExpression(keyExpression string, net *chaincfg.Params) (*KeyInfo, error) {
	matches := multisigDescriptorKeyRegexp.FindStringSubmatch(keyExpression)
	if matches == nil {
		return nil, errp.New(
			"unsupported key expression, expected [fingerprint/path]xpub/<0;1>/*")
	}
	rootFingerprint, err := hex.DecodeString(matches[1])
	if err != nil {
		return nil, errp.WithStack(err)
	}
	absoluteKeypath := NewEmptyAbsoluteKeypath()
	if matches[2] != "" {
		normalizedPath := strings.NewReplacer("h", hardenedKeySymbol, "H", hardenedKeySymbol).
			Replace(matches[2])
		absoluteKeypath, err = NewAbsoluteKeypath("m" + normalizedPath)
		if err != nil {
			return nil, err
		}
	}
	extendedPublicKey, err := hdkeychain.NewKeyFromString(matches[3])
	if err != nil {
		return nil, errp.Wrap(err, "could not read an extended public key")
	}
	if extendedPublicKey.IsPrivate() {
		return nil, errp.New("extended key must be public")
	}
	if !extendedPublicKey.IsForNet(net) {
		return nil, errp.New("extended public key is for a different network")
	}
	// Extended keys are always stored with the mainnet version bytes.
	extendedPublicKey.SetNet(&chaincfg.MainNetParams)
	return &KeyInfo{
		RootFingerprint:   rootFingerprint,
		AbsoluteKeypath:   absoluteKeypath,
		ExtendedPublicKey: extendedPublicKey,
	}, nil
}

// ParseBitcoinMultisigDescriptor parses a descriptor of the form
// wsh(sortedmulti(k,[fingerprint/path]xpub-or-tpub/<0;1>/*,...))#checksum. The checksum is
// mandatory, and the extended keys must be encoded for the given network.
func ParseBitcoinMultisigDescriptor(
	descriptor string,
	net *chaincfg.Params,
) (*BitcoinMultisig, error) {
	if net == nil {
		return nil, errp.New("network is nil")
	}
	payload, checksum, hasChecksum := strings.Cut(strings.TrimSpace(descriptor), "#")
	if !hasChecksum {
		return nil, errp.New("descriptor checksum is missing")
	}
	expectedChecksum, err := descriptorChecksum(payload)
	if err != nil {
		return nil, err
	}
	if checksum != expectedChecksum {
		return nil, errp.New("descriptor checksum does not match")
	}
	if !strings.HasPrefix(payload, multisigDescriptorPrefix) ||
		!strings.HasSuffix(payload, multisigDescriptorSuffix) ||
		len(payload) < len(multisigDescriptorPrefix)+len(multisigDescriptorSuffix) {
		return nil, errp.New("only wsh(sortedmulti(...)) descriptors are supported")
	}
	args := strings.Split(
		payload[len(multisigDescriptorPrefix):len(payload)-len(multisigDescriptorSuffix)], ",")
	threshold, err := strconv.Atoi(args[0])
	if err != nil || strconv.Itoa(threshold) != args[0] {
		return nil, errp.New("invalid threshold")
	}
	keys := make([]KeyInfo, 0, len(args)-1)
	for _, keyExpression := range args[1:] {
		key, err := parseDescriptorKeyExpression(keyExpression, net)
		if err != nil {
			return nil, err
		}
		keys = append(keys, *key)
	}
	multisig := &BitcoinMultisig{Threshold: threshold, Keys: keys}
	if err := multisig.Validate(); err != nil {
		return nil, err
	}
	return multisig, nil
}
