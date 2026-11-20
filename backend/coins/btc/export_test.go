// SPDX-License-Identifier: Apache-2.0

package btc

import "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/btc/maketx"

// TstSetActiveTxProposal sets the active transaction proposal, as TxProposal() does. It lets
// tests spend outputs that are not part of the synced transactions of the account.
func (account *Account) TstSetActiveTxProposal(txProposal *maketx.TxProposal) {
	defer account.activeTxProposalLock.Lock()()
	account.activeTxProposal = txProposal
}
