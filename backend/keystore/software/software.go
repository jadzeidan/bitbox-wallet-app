// SPDX-License-Identifier: Apache-2.0

package software

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/types"
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/eth"
	keystorePkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/keystore"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/logging"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil/v2/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg/v2"
	"github.com/btcsuite/btcd/chainhash/v2"
	"github.com/btcsuite/btcd/psbt/v2"
	"github.com/btcsuite/btcd/txscript/v2"
	"github.com/btcsuite/btcd/wire/v2"
	"github.com/ethereum/go-ethereum/accounts"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/pbkdf2"
)

// Keystore implements a keystore in software.
type Keystore struct {
	observable.Implementation

	// The master extended private key from which all keys are derived.
	master     *hdkeychain.ExtendedKey
	identifier string
	edition    Edition
	log        *logrus.Entry

	// registeredMultisigs holds the multisig accounts registered on this keystore. Registrations
	// are not persisted: they are lost when the keystore is disconnected.
	registeredMultisigs     map[string]struct{}
	registeredMultisigsLock sync.Mutex
}

// Edition models the coin set supported by the software keystore.
type Edition string

const (
	// EditionMulti supports all coins that the software keystore can sign for.
	EditionMulti Edition = "multi"
	// EditionBTCOnly only supports Bitcoin accounts.
	EditionBTCOnly Edition = "btc-only"
)

// NewKeystore creates a new keystore with the given configuration, index and key.
func NewKeystore(
	master *hdkeychain.ExtendedKey,
) *Keystore {
	return NewKeystoreWithEdition(master, EditionMulti)
}

// NewKeystoreWithEdition creates a new keystore with the given edition.
func NewKeystoreWithEdition(
	master *hdkeychain.ExtendedKey,
	edition Edition,
) *Keystore {
	if edition == "" {
		edition = EditionMulti
	}
	publicKey, _ := master.ECPubKey()
	hash := sha256.Sum256(publicKey.SerializeCompressed())
	return &Keystore{
		master:     master,
		identifier: hex.EncodeToString(hash[:]),
		edition:    edition,
		log:        logging.Get().WithGroup("software"),
	}
}

// NewKeystoreFromPINWithEdition creates a new unique keystore derived from the PIN.
func NewKeystoreFromPINWithEdition(pin string, edition Edition) *Keystore {
	seed := pbkdf2.Key([]byte(pin), []byte("BitBox"), 64, hdkeychain.RecommendedSeedLen, sha256.New)
	master, err := hdkeychain.NewMaster(seed, &chaincfg.TestNet3Params)
	if err != nil {
		panic(errp.WithStack(err))
	}
	return NewKeystoreWithEdition(master, edition)
}

// Type implements keystore.Keystore.
func (keystore *Keystore) Type() keystorePkg.Type {
	return keystorePkg.TypeSoftware
}

// Name implements keystore.Keystore.
func (keystore *Keystore) Name() (string, error) {
	fingerprint, err := keystore.RootFingerprint()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Software keystore %x", fingerprint), nil
}

// RootFingerprint implements keystore.Keystore.
func (keystore *Keystore) RootFingerprint() ([]byte, error) {
	// The bip32 Go lib we use does not expose a key's fingerprint. We simply get an arbitrary child
	// xpub and read the parentFingerprint field. This is part of the BIP32 specification.
	keypath, err := signing.NewAbsoluteKeypath("m/84'")
	if err != nil {
		return nil, err
	}
	xprv, err := keypath.Derive(keystore.master)
	if err != nil {
		return nil, err
	}
	fingerprint := make([]byte, 4)
	binary.BigEndian.PutUint32(fingerprint, xprv.ParentFingerprint())
	return fingerprint, nil
}

// DeterministicEntropy implements keystore.Keystore.
func (keystore *Keystore) DeterministicEntropy() ([]byte, error) {
	return nil, errp.New("deterministic entropy not supported by software keystore")
}

// SupportsDeterministicEntropy implements keystore.Keystore.
func (keystore *Keystore) SupportsDeterministicEntropy() bool {
	return false
}

// Configuration implements keystore.Keystore.
func (keystore *Keystore) Configuration() *signing.Configuration {
	return nil
}

// SupportsCoin implements keystore.Keystore.
func (keystore *Keystore) SupportsCoin(coin coinpkg.Coin) bool {
	if keystore.edition == EditionBTCOnly && (coin == nil || !coinpkg.IsBitcoinOnly(coin.Code())) {
		return false
	}
	switch coin.(type) {
	case *btc.Coin, *eth.Coin:
		return true
	default:
		return false
	}
}

// SupportsAccount implements keystore.Keystore.
func (keystore *Keystore) SupportsAccount(coin coinpkg.Coin, meta interface{}) bool {
	if !keystore.SupportsCoin(coin) {
		return false
	}
	switch coin.(type) {
	case *btc.Coin:
		scriptType := meta.(signing.ScriptType)
		if scriptType == signing.ScriptTypeP2WSH {
			// Multisig accounts are only offered for Bitcoin.
			return coinpkg.IsBitcoinOnly(coin.Code())
		}
		return scriptType == signing.ScriptTypeP2PKH ||
			scriptType == signing.ScriptTypeP2WPKHP2SH ||
			scriptType == signing.ScriptTypeP2WPKH ||
			scriptType == signing.ScriptTypeP2TR
	case *eth.Coin:
		return true
	default:
		return false
	}
}

// Identifier implements keystore.Keystore.
func (keystore *Keystore) Identifier() (string, error) {
	return keystore.identifier, nil
}

// CanVerifyAddress implements keystore.Keystore.
func (keystore *Keystore) CanVerifyAddress(coinpkg.Coin) (bool, bool, error) {
	return false, false, nil
}

// VerifyAddressBTC implements keystore.Keystore.
func (keystore *Keystore) VerifyAddressBTC(*signing.Configuration, types.Derivation, coinpkg.Coin) error {
	return errp.New("The software-based keystore has no secure output to display the address.")
}

// multisigRegistrationKey identifies a multisig account by its threshold and keys, independently
// of the order of the keys. An error is returned if this keystore does not hold one of the keys.
func (keystore *Keystore) multisigRegistrationKey(multisig *signing.BitcoinMultisig) (string, error) {
	if err := multisig.Validate(); err != nil {
		return "", err
	}
	rootFingerprint, err := keystore.RootFingerprint()
	if err != nil {
		return "", err
	}
	if multisig.KeyIndex(rootFingerprint) == -1 {
		return "", errp.New("the keystore does not hold a key of this multisig account")
	}
	xpubs := make([]string, len(multisig.Keys))
	for index, key := range multisig.Keys {
		xpubs[index] = key.ExtendedPublicKey.String()
	}
	sort.Strings(xpubs)
	return fmt.Sprintf("%d:%s", multisig.Threshold, strings.Join(xpubs, ",")), nil
}

// BTCMultisigRegistered implements keystore.Keystore.
func (keystore *Keystore) BTCMultisigRegistered(
	_ coinpkg.Coin, multisig *signing.BitcoinMultisig) (bool, error) {
	key, err := keystore.multisigRegistrationKey(multisig)
	if err != nil {
		return false, err
	}
	keystore.registeredMultisigsLock.Lock()
	defer keystore.registeredMultisigsLock.Unlock()
	_, registered := keystore.registeredMultisigs[key]
	return registered, nil
}

// RegisterBTCMultisig implements keystore.Keystore. There is nothing to confirm for the user, the
// account is registered right away.
func (keystore *Keystore) RegisterBTCMultisig(
	_ coinpkg.Coin, multisig *signing.BitcoinMultisig, _ string) error {
	key, err := keystore.multisigRegistrationKey(multisig)
	if err != nil {
		return err
	}
	keystore.registeredMultisigsLock.Lock()
	defer keystore.registeredMultisigsLock.Unlock()
	if keystore.registeredMultisigs == nil {
		keystore.registeredMultisigs = map[string]struct{}{}
	}
	keystore.registeredMultisigs[key] = struct{}{}
	return nil
}

// VerifyAddressETH implements keystore.Keystore.
func (keystore *Keystore) VerifyAddressETH(*signing.Configuration, coinpkg.Coin) error {
	return errp.New("The software-based keystore has no secure output to display the address.")
}

// CanVerifyExtendedPublicKey implements keystore.Keystore.
func (keystore *Keystore) CanVerifyExtendedPublicKey() bool {
	return false
}

// VerifyExtendedPublicKey implements keystore.Keystore.
func (keystore *Keystore) VerifyExtendedPublicKey(coin coinpkg.Coin, configuration *signing.Configuration) error {
	return errp.New("The software-based keystore has no secure output to display the public key.")
}

// ExtendedPublicKey implements keystore.Keystore.
func (keystore *Keystore) ExtendedPublicKey(
	coin coinpkg.Coin, absoluteKeypath signing.AbsoluteKeypath,
) (*hdkeychain.ExtendedKey, error) {
	extendedPrivateKey, err := absoluteKeypath.Derive(keystore.master)
	if err != nil {
		return nil, err
	}
	return extendedPrivateKey.Neuter()
}

// BTCXPubs implements keystore.Keystore.
func (keystore *Keystore) BTCXPubs(
	coin coinpkg.Coin, keypaths []signing.AbsoluteKeypath) ([]*hdkeychain.ExtendedKey, error) {
	xpubs := make([]*hdkeychain.ExtendedKey, len(keypaths))
	for i, keypath := range keypaths {
		xpub, err := keystore.ExtendedPublicKey(coin, keypath)
		if err != nil {
			return nil, err
		}
		xpubs[i] = xpub
	}
	return xpubs, nil
}

// Features reports optional capabilities supported by the software keystore.
func (keystore *Keystore) Features() *keystorePkg.Features {
	return &keystorePkg.Features{
		SupportsSendToSelf: true,
	}
}

// signBTCMultisigTransaction adds the signature of this keystore to every input of a transaction
// that spends from a multisig account. The PSBT states the witness script and the key of every
// cosigner for each input. Signatures of other cosigners are kept.
func (keystore *Keystore) signBTCMultisigTransaction(packet *psbt.Packet) error {
	rootFingerprint, err := keystore.RootFingerprint()
	if err != nil {
		return err
	}
	fingerprint := binary.LittleEndian.Uint32(rootFingerprint)
	transaction := packet.UnsignedTx

	prevOutputs := map[wire.OutPoint]*wire.TxOut{}
	for index, txIn := range transaction.TxIn {
		utxo := packet.Inputs[index].WitnessUtxo
		if utxo == nil {
			return errp.Newf("input %d: the output being spent is missing", index)
		}
		prevOutputs[txIn.PreviousOutPoint] = utxo
	}
	sigHashes := txscript.NewTxSigHashes(transaction, txscript.NewMultiPrevOutFetcher(prevOutputs))

	for index := range transaction.TxIn {
		input := &packet.Inputs[index]
		var derivation *psbt.Bip32Derivation
		for _, candidate := range input.Bip32Derivation {
			if candidate.MasterKeyFingerprint == fingerprint {
				derivation = candidate
				break
			}
		}
		if derivation == nil {
			return errp.Newf("input %d: the key of this keystore is missing", index)
		}
		xprv := keystore.master
		for _, child := range derivation.Bip32Path {
			xprv, err = xprv.Derive(child)
			if err != nil {
				return errp.WithStack(err)
			}
		}
		prv, err := xprv.ECPrivKey()
		if err != nil {
			return errp.WithStack(err)
		}
		if !bytes.Equal(prv.PubKey().SerializeCompressed(), derivation.PubKey) {
			return errp.Newf("input %d: the key does not belong to this keystore", index)
		}
		signatureHash, err := txscript.CalcWitnessSigHash(
			input.WitnessScript, sigHashes, txscript.SigHashAll,
			transaction, index, input.WitnessUtxo.Value)
		if err != nil {
			return errp.Wrap(err, "Failed to calculate SegWit signature hash")
		}
		partialSig := &psbt.PartialSig{
			PubKey:    derivation.PubKey,
			Signature: append(ecdsa.Sign(prv, signatureHash).Serialize(), byte(txscript.SigHashAll)),
		}
		replaced := false
		for sigIndex, existing := range input.PartialSigs {
			if bytes.Equal(existing.PubKey, derivation.PubKey) {
				input.PartialSigs[sigIndex] = partialSig
				replaced = true
			}
		}
		if !replaced {
			input.PartialSigs = append(input.PartialSigs, partialSig)
		}
	}
	return nil
}

func (keystore *Keystore) signBTCTransaction(btcProposedTx *btc.ProposedTransaction) error {
	keystore.log.Info("Sign transaction.")
	if btcProposedTx.AccountSigningConfigurations.Multisig() != nil {
		return keystore.signBTCMultisigTransaction(btcProposedTx.TXProposal.Psbt)
	}
	transaction := btcProposedTx.TXProposal.Psbt.UnsignedTx
	sigHashes := btcProposedTx.TXProposal.SigHashes()
	for index, txIn := range transaction.TxIn {
		spentOutput, ok := btcProposedTx.TXProposal.PreviousOutputs[txIn.PreviousOutPoint]
		if !ok {
			keystore.log.Error("There needs to be exactly one output being spent per input.")
			return errp.New("There needs to be exactly one output being spent per input.")
		}
		addressID := spentOutput.Address.PubkeyScriptHashHex()
		address, err := btcProposedTx.GetKeystoreAddress(
			btcProposedTx.TXProposal.Coin.Code(),
			addressID,
		)
		if err != nil {
			return err
		}

		xprv, err := address.AbsoluteKeypath().Derive(keystore.master)
		if err != nil {
			return err
		}
		prv, err := xprv.ECPrivKey()
		if err != nil {
			return errp.WithStack(err)
		}

		if address.AccountConfiguration.ScriptType() == signing.ScriptTypeP2TR {
			prv = txscript.TweakTaprootPrivKey(*prv, nil)
			signatureHash, err := txscript.CalcTaprootSignatureHash(
				sigHashes, txscript.SigHashDefault, transaction,
				index, btcProposedTx.TXProposal.PreviousOutputs)
			if err != nil {
				return errp.Wrap(err, "Failed to calculate Taproot signature hash")
			}
			keystore.log.Debug("Calculated taproot signature hash")
			signature, err := schnorr.Sign(prv, signatureHash)
			if err != nil {
				return err
			}
			btcProposedTx.TXProposal.Psbt.Inputs[index].TaprootKeySpendSig = signature.Serialize()
		} else {
			var signatureHash []byte
			isSegwit, subScript := address.ScriptForHashToSign()
			if isSegwit {
				var err error
				signatureHash, err = txscript.CalcWitnessSigHash(subScript, sigHashes,
					txscript.SigHashAll, transaction, index, spentOutput.TxOut.Value)
				if err != nil {
					return errp.Wrap(err, "Failed to calculate SegWit signature hash")
				}
				keystore.log.Debug("Calculated segwit signature hash")
			} else {
				var err error
				signatureHash, err = txscript.CalcSignatureHash(
					subScript, txscript.SigHashAll, transaction, index)
				if err != nil {
					return errp.Wrap(err, "Failed to calculate legacy signature hash")
				}
				keystore.log.Debug("Calculated legacy signature hash")
			}
			signature := ecdsa.Sign(prv, signatureHash).Serialize()
			btcProposedTx.TXProposal.Psbt.Inputs[index].PartialSigs = []*psbt.PartialSig{
				{
					PubKey:    prv.PubKey().SerializeCompressed(),
					Signature: append(signature, byte(txscript.SigHashAll)),
				},
			}
		}
	}

	return nil
}

func (keystore *Keystore) signETHTransaction(tx *eth.TxProposal) error {
	xprv, err := tx.Keypath.Derive(keystore.master)
	if err != nil {
		return errp.Newf("failed to derive key: %v", err)
	}
	privKey, err := xprv.ECPrivKey()
	if err != nil {
		return errp.Newf("failed to get private key: %v", err)
	}
	tx.Tx, err = ethTypes.SignTx(tx.Tx, tx.Signer(), privKey.ToECDSA())
	return err
}

// SignTransaction implements keystore.Keystore.
func (keystore *Keystore) SignTransaction(
	proposedTransaction interface{},
) error {
	switch specificProposedTx := proposedTransaction.(type) {
	case *btc.ProposedTransaction:
		if keystore.edition == EditionBTCOnly &&
			!coinpkg.IsBitcoinOnly(specificProposedTx.TXProposal.Coin.Code()) {
			return errp.Newf("coin not supported: %s", specificProposedTx.TXProposal.Coin.Code())
		}
		return keystore.signBTCTransaction(specificProposedTx)
	case *eth.TxProposal:
		if keystore.edition == EditionBTCOnly {
			return errp.New("coin not supported: eth")
		}
		return keystore.signETHTransaction(specificProposedTx)
	default:
		panic("unknown proposal type")
	}
}

// CanSignMessage implements keystore.Keystore.
func (keystore *Keystore) CanSignMessage(code coinpkg.Code) bool {
	if coinpkg.IsBitcoinOnly(code) {
		return true
	}
	return keystore.edition == EditionMulti &&
		(code == coinpkg.CodeETH || code == coinpkg.CodeSEPETH)
}

func btcMessageHash(message []byte) ([]byte, error) {
	var serialized bytes.Buffer
	if err := wire.WriteVarString(&serialized, 0, "Bitcoin Signed Message:\n"); err != nil {
		return nil, err
	}
	if err := wire.WriteVarString(&serialized, 0, string(message)); err != nil {
		return nil, err
	}
	return chainhash.DoubleHashB(serialized.Bytes()), nil
}

// SignBTCMessage implements keystore.Keystore.
func (keystore *Keystore) SignBTCMessage(message []byte, keypath signing.AbsoluteKeypath, scriptType signing.ScriptType, coinCode coinpkg.Code) ([]byte, error) {
	if scriptType == signing.ScriptTypeP2TR {
		return nil, errp.New("taproot not supported")
	}
	if !coinpkg.IsBitcoinOnly(coinCode) {
		return nil, errp.Newf("coin not supported: %s", coinCode)
	}
	xprv, err := keypath.Derive(keystore.master)
	if err != nil {
		return nil, err
	}
	prv, err := xprv.ECPrivKey()
	if err != nil {
		return nil, errp.WithStack(err)
	}
	msgHash, err := btcMessageHash(message)
	if err != nil {
		return nil, err
	}
	return ecdsa.SignCompact(prv, msgHash, true), nil
}

// SignETHMessage implements keystore.Keystore.
func (keystore *Keystore) SignETHMessage(chainID uint64, message []byte, keypath signing.AbsoluteKeypath) ([]byte, error) {
	_ = chainID
	if keystore.edition == EditionBTCOnly {
		return nil, errp.New("coin not supported: eth")
	}
	xprv, err := keypath.Derive(keystore.master)
	if err != nil {
		return nil, err
	}
	prv, err := xprv.ECPrivKey()
	if err != nil {
		return nil, errp.WithStack(err)
	}
	signature, err := crypto.Sign(accounts.TextHash(message), prv.ToECDSA())
	if err != nil {
		return nil, err
	}
	// 27 is the magic constant to add to the recoverable ID to denote an uncompressed pubkey.
	signature[64] += 27
	return signature, nil
}

// SignETHTypedMessage implements keystore.Keystore.
func (keystore *Keystore) SignETHTypedMessage(chainID uint64, data []byte, keypath signing.AbsoluteKeypath) ([]byte, error) {
	return nil, errp.New("unsupported")
}

// SupportsEIP1559 implements keystore.Keystore.
func (keystore *Keystore) SupportsEIP1559() bool {
	return false
}

// SupportsFeature implements keystore.Keystore.
func (keystore *Keystore) SupportsFeature(feature keystorePkg.Feature) error {
	switch feature {
	case keystorePkg.FeatureBTCTransactionSigning,
		keystorePkg.FeatureETHTransactionSigning,
		keystorePkg.FeatureMessageSigning:
		return nil
	default:
		return keystorePkg.ErrUnsupportedFeature
	}
}
