// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"fmt"

	accountsTypes "github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
)

// The functions here must all produce globally unique account codes. They are used as names in
// account-related databases (e.g. transaction notes). Changing the codes invalidates these
// databases.
//
// There are different types of account codes:
// - regular: for unified accounts
// - token: for token accounts (e.g. ERC20/SPL)

// regularAccountCode returns an account code based on a keystore root fingerprint, a coin code and
// an account number.
func regularAccountCode(rootFingerprint []byte, coinCode coin.Code, accountNumber uint16) accountsTypes.Code {
	return accountsTypes.Code(fmt.Sprintf("v0-%x-%s-%d", rootFingerprint, coinCode, accountNumber))
}

// TokenAccountCode returns the account code used for token accounts.
// It is derived from the account code of the parent account and the token code.
func TokenAccountCode(parentAccountCode accountsTypes.Code, tokenCode string) accountsTypes.Code {
	return accountsTypes.Code(fmt.Sprintf("%s-%s", parentAccountCode, tokenCode))
}

// Erc20AccountCode returns the account code used for an ERC20 token.
// Kept for backwards compatibility with existing callers/tests.
func Erc20AccountCode(ethereumAccountCode accountsTypes.Code, tokenCode string) accountsTypes.Code {
	return TokenAccountCode(ethereumAccountCode, tokenCode)
}
