// SPDX-License-Identifier: Apache-2.0

package addresses

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/blockchain"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	ourbtcutil "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/util"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	addresspkg "github.com/btcsuite/btcd/address/v2"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/sirupsen/logrus"
)

// AddressID identifies a BTC/LTC address at the account/frontend boundary.
// It is permanently identical to the address scriptHashHex.
type AddressID = blockchain.ScriptHashHex

// NewAddressID creates an address ID from a pubkey script.
func NewAddressID(pubkeyScript []byte) AddressID {
	return blockchain.NewScriptHashHex(pubkeyScript)
}

// AccountAddress models an address that belongs to an account of the user.
// It contains all the information needed to receive and spend funds.
type AccountAddress struct {
	addresspkg.Address

	// AccountConfiguration is the account level configuration from which this address was derived.
	AccountConfiguration *signing.Configuration
	// PublicKey is the public key of a single-sig address. It is nil for multisig addresses.
	PublicKey  *btcec.PublicKey
	Derivation types.Derivation

	// redeemScript stores the redeem script of a BIP16 P2SH output or nil if address type is not
	// P2SH.
	RedeemScript []byte
	// WitnessScript stores the witness script of a P2WSH output or nil if the address type is not
	// P2WSH.
	WitnessScript []byte

	log *logrus.Entry
}

// multisigWitnessScript returns the witness script of a multisig address:
// `threshold <public keys> n OP_CHECKMULTISIG`. The public keys are derived from the account-level
// key of each cosigner and sorted lexicographically, as defined for sortedmulti() descriptors.
func multisigWitnessScript(
	multisig *signing.BitcoinMultisig,
	relativeKeypath signing.RelativeKeypath,
) ([]byte, error) {
	if err := multisig.Validate(); err != nil {
		return nil, err
	}
	publicKeys := make([][]byte, len(multisig.Keys))
	for index, key := range multisig.Keys {
		derivedXpub, err := relativeKeypath.Derive(key.ExtendedPublicKey)
		if err != nil {
			return nil, err
		}
		publicKey, err := derivedXpub.ECPubKey()
		if err != nil {
			return nil, err
		}
		publicKeys[index] = publicKey.SerializeCompressed()
	}
	slices.SortFunc(publicKeys, bytes.Compare)

	builder := txscript.NewScriptBuilder().AddInt64(int64(multisig.Threshold))
	for _, publicKey := range publicKeys {
		builder.AddData(publicKey)
	}
	return builder.
		AddInt64(int64(len(publicKeys))).
		AddOp(txscript.OP_CHECKMULTISIG).
		Script()
}

// newMultisigAccountAddress creates a new P2WSH multisig account address.
func newMultisigAccountAddress(
	accountConfiguration *signing.Configuration,
	derivation types.Derivation,
	relativeKeypath signing.RelativeKeypath,
	net *chaincfg.Params,
	log *logrus.Entry,
) *AccountAddress {
	witnessScript, err := multisigWitnessScript(accountConfiguration.BitcoinMultisig, relativeKeypath)
	if err != nil {
		log.WithError(err).Panic("Failed to create the multisig witness script.")
	}
	witnessScriptHash := sha256.Sum256(witnessScript)
	address, err := addresspkg.NewAddressWitnessScriptHash(witnessScriptHash[:], net)
	if err != nil {
		log.WithError(err).Panic("Failed to get p2wsh addr. from witness script hash.")
	}
	return &AccountAddress{
		Address:              address,
		AccountConfiguration: accountConfiguration,
		Derivation:           derivation,
		WitnessScript:        witnessScript,
		log:                  log,
	}
}

// MultisigKeyDerivation is the key one cosigner contributes to a multisig address.
type MultisigKeyDerivation struct {
	// RootFingerprint identifies the keystore of the cosigner.
	RootFingerprint []byte
	// AbsoluteKeypath is the keypath of the public key.
	AbsoluteKeypath signing.AbsoluteKeypath
	PublicKey       *btcec.PublicKey
}

// MultisigKeyDerivations returns the key every cosigner contributes to this multisig address, in
// the order of the keys of the account configuration. A keystore needs these to find its key when
// signing.
func (address *AccountAddress) MultisigKeyDerivations() ([]MultisigKeyDerivation, error) {
	multisig := address.AccountConfiguration.BitcoinMultisig
	if multisig == nil {
		return nil, errp.New("not a multisig address")
	}
	relativeKeypath := signing.NewEmptyRelativeKeypath().
		Child(address.Derivation.SimpleChainIndex(), signing.NonHardened).
		Child(address.Derivation.AddressIndex, signing.NonHardened)
	result := make([]MultisigKeyDerivation, len(multisig.Keys))
	for index, key := range multisig.Keys {
		derivedXpub, err := relativeKeypath.Derive(key.ExtendedPublicKey)
		if err != nil {
			return nil, err
		}
		publicKey, err := derivedXpub.ECPubKey()
		if err != nil {
			return nil, errp.WithStack(err)
		}
		result[index] = MultisigKeyDerivation{
			RootFingerprint: key.RootFingerprint,
			AbsoluteKeypath: key.AbsoluteKeypath.
				Child(address.Derivation.SimpleChainIndex(), signing.NonHardened).
				Child(address.Derivation.AddressIndex, signing.NonHardened),
			PublicKey: publicKey,
		}
	}
	return result, nil
}

// NewAccountAddress creates a new account address.
func NewAccountAddress(
	accountConfiguration *signing.Configuration,
	derivation types.Derivation,
	net *chaincfg.Params,
	log *logrus.Entry,
) *AccountAddress {

	log = log.WithFields(logrus.Fields{
		"accountConfiguration": accountConfiguration.String(),
		"change":               derivation.Change,
		"addressIndex":         derivation.AddressIndex,
	})
	log.Debug("Creating new account address")

	var address addresspkg.Address
	var redeemScript []byte
	relativeKeypath := signing.NewEmptyRelativeKeypath().
		Child(derivation.SimpleChainIndex(), signing.NonHardened).
		Child(derivation.AddressIndex, signing.NonHardened)
	if accountConfiguration.IsMultisig() {
		return newMultisigAccountAddress(accountConfiguration, derivation, relativeKeypath, net, log)
	}
	derivedXpub, err := relativeKeypath.Derive(accountConfiguration.ExtendedPublicKey())
	if err != nil {
		log.WithError(err).Panic("Failed to derive xpub.")
	}
	publicKey, err := derivedXpub.ECPubKey()
	if err != nil {
		log.WithError(err).Panic("Failed to convert an extended public key to a normal public key.")
	}

	publicKeyHash := addresspkg.Hash160(publicKey.SerializeCompressed())
	switch accountConfiguration.ScriptType() {
	case signing.ScriptTypeP2PKH:
		address, err = addresspkg.NewAddressPubKeyHash(publicKeyHash, net)
		if err != nil {
			log.WithError(err).Panic("Failed to get P2PKH addr. from public key hash.")
		}
	case signing.ScriptTypeP2WPKHP2SH:
		var segwitAddress *addresspkg.AddressWitnessPubKeyHash
		segwitAddress, err = addresspkg.NewAddressWitnessPubKeyHash(publicKeyHash, net)
		if err != nil {
			log.WithError(err).Panic("Failed to get p2wpkh-p2sh addr. from publ. key hash.")
		}
		redeemScript, err = txscript.PayToAddrScript(segwitAddress)
		if err != nil {
			log.WithError(err).Panic("Failed to get redeem script for segwit address.")
		}
		address, err = addresspkg.NewAddressScriptHash(redeemScript, net)
		if err != nil {
			log.WithError(err).Panic("Failed to get a P2SH address for segwit.")
		}
	case signing.ScriptTypeP2WPKH:
		address, err = addresspkg.NewAddressWitnessPubKeyHash(publicKeyHash, net)
		if err != nil {
			log.WithError(err).Panic("Failed to get p2wpkh addr. from publ. key hash.")
		}
	case signing.ScriptTypeP2TR:
		outputKey := txscript.ComputeTaprootKeyNoScript(publicKey)
		address, err = addresspkg.NewAddressTaproot(schnorr.SerializePubKey(outputKey), net)
		if err != nil {
			log.WithError(err).Panic("Failed to get p2tr addr")
		}
	default:
		log.Panic(fmt.Sprintf("Unrecognized script type: %s", accountConfiguration.ScriptType()))
	}

	return &AccountAddress{
		Address:              address,
		AccountConfiguration: accountConfiguration,
		PublicKey:            publicKey,
		Derivation:           derivation,
		RedeemScript:         redeemScript,
		log:                  log,
	}
}

// ID implements accounts.Address.
// For BTC/LTC, this value must never change because it is treated interchangeably with the
// address scriptHashHex.
func (address *AccountAddress) ID() string {
	return string(address.PubkeyScriptHashHex())
}

// EncodeForHumans implements accounts.Address.
func (address *AccountAddress) EncodeForHumans() string {
	return address.EncodeAddress()
}

// AbsoluteKeypath implements accounts.Address. Must not be called on multisig addresses, which
// have one keypath per cosigner.
func (address *AccountAddress) AbsoluteKeypath() signing.AbsoluteKeypath {
	return address.AccountConfiguration.AbsoluteKeypath().
		Child(address.Derivation.SimpleChainIndex(), false).
		Child(address.Derivation.AddressIndex, false)
}

// PubkeyScript returns the pubkey script of this address. Use this in a tx output to receive funds.
func (address *AccountAddress) PubkeyScript() []byte {
	script, err := ourbtcutil.PkScriptFromAddress(address.Address)
	if err != nil {
		address.log.WithError(err).Panic("Failed to get the pubkey script for an address.")
	}
	return script
}

// PubkeyScriptHashHex returns the hash of the pubkey script in hex format.
// It is used to subscribe to notifications at the ElectrumX server.
func (address *AccountAddress) PubkeyScriptHashHex() blockchain.ScriptHashHex {
	return blockchain.NewScriptHashHex(address.PubkeyScript())
}

// ScriptForHashToSign returns whether this address is a segwit output and the script used when
// calculating the hash to be signed in a transaction. This info is needed when trying to spend
// from this address.
func (address *AccountAddress) ScriptForHashToSign() (bool, []byte) {
	switch address.AccountConfiguration.ScriptType() {
	case signing.ScriptTypeP2PKH:
		return false, address.PubkeyScript()
	case signing.ScriptTypeP2WPKHP2SH:
		return true, address.RedeemScript
	case signing.ScriptTypeP2WPKH:
		return true, address.PubkeyScript()
	default:
		address.log.Panic("Unrecognized address type.")
	}
	panic("The end of the function cannot be reached.")
}
