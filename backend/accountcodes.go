// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"slices"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
)

// The functions here must all produce globally unique account codes. They are used as names in
// account-related databases (e.g. transaction notes). Changing the codes invalidates these
// databases.
//
// There are different types of account codes:
// - regular: for unified accounts
// - erc20: for ERC20 token accounts
// - vault: for multisig accounts

// regularAccountCode returns an account code based on a keystore root fingerprint, a coin code and
// an account number.
func regularAccountCode(rootFingerprint []byte, coinCode coin.Code, accountNumber uint16) accountsTypes.Code {
	return accountsTypes.Code(fmt.Sprintf("v0-%x-%s-%d", rootFingerprint, coinCode, accountNumber))
}

// Erc20AccountCode returns the account code used for an ERC20 token.
// It is derived from the account code of the parent ETH account and the token code.
func Erc20AccountCode(ethereumAccountCode accountsTypes.Code, tokenCode string) accountsTypes.Code {
	return accountsTypes.Code(fmt.Sprintf("%s-%s", ethereumAccountCode, tokenCode))
}

// vaultAccountCode returns the account code of a vault. It is derived from the threshold and the
// keys of the multisig configuration. The order of the keys and their origin metadata do not
// affect the addresses of the vault, so they do not affect the code either: the same vault always
// has the same code.
func vaultAccountCode(
	coinCode coin.Code,
	multisig *signing.BitcoinMultisig,
) (accountsTypes.Code, error) {
	keys := make([][]byte, len(multisig.Keys))
	for index, key := range multisig.Keys {
		publicKey, err := key.ExtendedPublicKey.ECPubKey()
		if err != nil {
			return "", errp.WithStack(err)
		}
		keys[index] = append(publicKey.SerializeCompressed(), key.ExtendedPublicKey.ChainCode()...)
	}
	slices.SortFunc(keys, bytes.Compare)
	hash := sha256.New()
	fmt.Fprintf(hash, "%d-of-%d", multisig.Threshold, len(keys))
	for _, key := range keys {
		hash.Write(key)
	}
	return accountsTypes.Code(fmt.Sprintf("v0-vault-%x-%s", hash.Sum(nil)[:8], coinCode)), nil
}
