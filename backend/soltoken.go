// SPDX-License-Identifier: Apache-2.0

package backend

import (
	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/sol"
)

type solToken struct {
	parentCode coinpkg.Code
	code       coinpkg.Code
	name       string
	unit       string
	token      *sol.Token
}

var solTokens = []solToken{
	{
		parentCode: coinpkg.CodeSOL,
		code:       "sol-spl-usdt",
		name:       "Tether USD",
		unit:       "USDT",
		token:      sol.NewToken("Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB", 6),
	},
	{
		parentCode: coinpkg.CodeSOL,
		code:       "sol-spl-usdc",
		name:       "USD Coin",
		unit:       "USDC",
		token:      sol.NewToken("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", 6),
	},
}

func solTokenByCode(code coinpkg.Code) *solToken {
	for _, token := range solTokens {
		if code == token.code {
			token := token
			return &token
		}
	}
	return nil
}
