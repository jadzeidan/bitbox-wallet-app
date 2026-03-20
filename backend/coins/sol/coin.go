// SPDX-License-Identifier: Apache-2.0

package sol

import (
	"math/big"
	"strings"

	coinpkg "github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/observable"
)

// Coin models Solana.
type Coin struct {
	observable.Implementation
	client                *RPCClient
	code                  coinpkg.Code
	name                  string
	unit                  string
	feeUnit               string
	blockExplorerTxPrefix string
	token                 *Token
}

// Token holds Solana SPL token details.
type Token struct {
	mint     string
	decimals uint8
}

// NewToken creates a new token metadata object.
func NewToken(mint string, decimals uint8) *Token {
	return &Token{
		mint:     mint,
		decimals: decimals,
	}
}

// Mint returns the token mint address.
func (token *Token) Mint() string {
	return token.mint
}

// Decimals returns the token decimals.
func (token *Token) Decimals() uint8 {
	return token.decimals
}

func NewCoin(
	client *RPCClient,
	code coinpkg.Code,
	name string,
	unit string,
	feeUnit string,
	blockExplorerTxPrefix string,
	token *Token,
) *Coin {
	return &Coin{
		client:                client,
		code:                  code,
		name:                  name,
		unit:                  unit,
		feeUnit:               feeUnit,
		blockExplorerTxPrefix: blockExplorerTxPrefix,
		token:                 token,
	}
}

// Client returns the RPC client.
func (coin *Coin) Client() *RPCClient {
	return coin.client
}

// Initialize implements coin.Coin.
func (coin *Coin) Initialize() {}

// Name implements coin.Coin.
func (coin *Coin) Name() string {
	return coin.name
}

// Code implements coin.Coin.
func (coin *Coin) Code() coinpkg.Code {
	return coin.code
}

// Unit implements coin.Coin.
func (coin *Coin) Unit(isFee bool) string {
	if isFee {
		return coin.feeUnit
	}
	return coin.unit
}

// GetFormatUnit implements coin.Coin.
func (coin *Coin) GetFormatUnit(isFee bool) string {
	return coin.Unit(isFee)
}

// Decimals implements coin.Coin.
func (coin *Coin) Decimals(isFee bool) uint {
	if !isFee && coin.token != nil {
		return uint(coin.token.Decimals())
	}
	return 9
}

func (coin *Coin) unitFactor(isFee bool) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(coin.Decimals(isFee))), nil)
}

// FormatAmount implements coin.Coin.
func (coin *Coin) FormatAmount(amount coinpkg.Amount, isFee bool) string {
	factor := coin.unitFactor(isFee)
	s := new(big.Rat).SetFrac(amount.BigInt(), factor).FloatString(int(coin.Decimals(isFee)))
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// ToUnit implements coin.Coin.
func (coin *Coin) ToUnit(amount coinpkg.Amount, isFee bool) float64 {
	factor := coin.unitFactor(isFee)
	result, _ := new(big.Rat).SetFrac(amount.BigInt(), factor).Float64()
	return result
}

// SetAmount implements coin.Coin.
func (coin *Coin) SetAmount(amount *big.Rat, isFee bool) coinpkg.Amount {
	factor := coin.unitFactor(isFee)
	lamports := new(big.Rat).Mul(amount, new(big.Rat).SetInt(factor))
	lamportsInt, _ := new(big.Int).SetString(lamports.FloatString(0), 10)
	return coinpkg.NewAmount(lamportsInt)
}

// ParseAmount implements coin.Coin.
func (coin *Coin) ParseAmount(amount string) (coinpkg.Amount, error) {
	amountRat, valid := new(big.Rat).SetString(amount)
	if !valid {
		return coinpkg.Amount{}, errp.New("Invalid amount")
	}
	return coin.SetAmount(amountRat, false), nil
}

// BlockExplorerTransactionURLPrefix implements coin.Coin.
func (coin *Coin) BlockExplorerTransactionURLPrefix() string {
	return coin.blockExplorerTxPrefix
}

// SmallestUnit implements coin.Coin.
func (coin *Coin) SmallestUnit() string {
	if coin.token != nil {
		return "base units"
	}
	return "lamports"
}

// Token returns nil for native SOL accounts, and token metadata for SPL token accounts.
func (coin *Coin) Token() *Token {
	return coin.token
}

// Close implements coin.Coin.
func (coin *Coin) Close() error {
	return nil
}
