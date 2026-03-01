// SPDX-License-Identifier: Apache-2.0

package sol

import "github.com/BitBoxSwiss/bitbox-wallet-app/backend/signing"

// Address represents a Solana address.
type Address struct {
	address string
	keypath signing.AbsoluteKeypath
}

func (addr Address) ID() string {
	return addr.address
}

func (addr Address) EncodeForHumans() string {
	return addr.address
}

func (addr Address) AbsoluteKeypath() signing.AbsoluteKeypath {
	return addr.keypath
}
