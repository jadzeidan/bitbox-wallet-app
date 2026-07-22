// SPDX-License-Identifier: Apache-2.0

package lightning

import (
	"encoding/json"
	"net/http"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts/types"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/jsonp"

	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/accounts"
	"github.com/BitBoxSwiss/bitbox-wallet-app/backend/coins/coin"
	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
	"github.com/gorilla/mux"
)

type responseDto struct {
	Success      bool        `json:"success"`
	Data         interface{} `json:"data"`
	ErrorMessage string      `json:"errorMessage,omitempty"`
	ErrorCode    string      `json:"errorCode,omitempty"`
}

// NewHandlers creates a new Handlers instance.
func NewHandlers(
	handleNoError func(string, func(*http.Request) interface{}) *mux.Route,
	lightning *Lightning,
) {
	handleNoError("/account", lightning.GetAccount).Methods("GET")
	handleNoError("/ready", lightning.GetReady).Methods("GET")
	handleNoError("/activate", lightning.PostActivate).Methods("POST")
	handleNoError("/deactivate", lightning.PostDeactivate).Methods("POST")
	handleNoError("/balance", lightning.GetBalance).Methods("GET")
	handleNoError("/block-explorer-tx-prefix", lightning.GetBlockExplorerTxPrefix).Methods("GET")
	handleNoError("/service-status", lightning.GetServiceStatus).Methods("GET")
	handleNoError("/list-payments", lightning.GetListPayments).Methods("GET")
	handleNoError("/credentials", lightning.GetCredentials).Methods("GET")
	handleNoError("/state", lightning.PostState).Methods("POST")
}

func errorResponse(err error) responseDto {
	if errCode, ok := errp.Cause(err).(errp.ErrorCode); ok {
		return responseDto{Success: false, ErrorCode: string(errCode)}
	}
	return responseDto{Success: false, ErrorMessage: err.Error()}
}

// GetAccount handles the GET request to retrieve the configured lightning account.
func (lightning *Lightning) GetAccount(_ *http.Request) interface{} {
	account := lightning.Account()
	type response struct {
		RootFingerprint jsonp.HexBytes `json:"rootFingerprint"`
		Code            types.Code     `json:"code"`
		Number          uint16         `json:"num"`
	}
	if account == nil {
		return nil
	}
	return &response{
		RootFingerprint: account.RootFingerprint,
		Code:            account.Code,
		Number:          account.Number,
	}
}

// GetBlockExplorerTxPrefix handles the GET request to retrieve the Bitcoin transaction explorer prefix.
func (lightning *Lightning) GetBlockExplorerTxPrefix(_ *http.Request) interface{} {
	return responseDto{
		Success: true,
		Data:    lightning.btcCoin.BlockExplorerTransactionURLPrefix(),
	}
}

// GetReady handles the GET request to retrieve whether the lightning wallet is ready.
func (lightning *Lightning) GetReady(_ *http.Request) interface{} {
	return responseDto{Success: true, Data: lightning.Ready()}
}

// PostActivate handles the POST request to activate lightning.
func (lightning *Lightning) PostActivate(_ *http.Request) interface{} {
	if err := lightning.Activate(); err != nil {
		lightning.log.Error(err)
		return errorResponse(err)
	}

	return responseDto{Success: true}
}

// PostDeactivate handles the POST request to deactivate lightning.
func (lightning *Lightning) PostDeactivate(_ *http.Request) interface{} {
	if err := lightning.Deactivate(); err != nil {
		lightning.log.Error(err)
		return errorResponse(err)
	}

	return responseDto{Success: true}
}

// GetBalance handles the GET request to retrieve the balance and its fiat conversions.
func (lightning *Lightning) GetBalance(_ *http.Request) interface{} {
	balance, err := lightning.Balance()
	if err != nil {
		return errorResponse(err)
	}

	btcCoin := lightning.btcCoin

	formattedAvailableAmount := coin.FormattedAmountWithConversions{
		Amount:      btcCoin.FormatAmount(balance.Available(), false),
		Unit:        btcCoin.GetFormatUnit(false),
		Conversions: coin.Conversions(balance.Available(), btcCoin, false, lightning.ratesUpdater),
	}
	formattedIncomingAmount := coin.FormattedAmountWithConversions{
		Amount:      btcCoin.FormatAmount(balance.Incoming(), false),
		Unit:        btcCoin.GetFormatUnit(false),
		Conversions: coin.Conversions(balance.Incoming(), btcCoin, false, lightning.ratesUpdater),
	}

	return responseDto{
		Success: true,
		Data: accounts.FormattedAccountBalance{
			HasAvailable: balance.Available().BigInt().Sign() > 0,
			Available:    formattedAvailableAmount,
			HasIncoming:  balance.Incoming().BigInt().Sign() > 0,
			Incoming:     formattedIncomingAmount,
		},
	}
}

// GetServiceStatus handles the GET request to retrieve the lightning service status.
func (lightning *Lightning) GetServiceStatus(_ *http.Request) interface{} {
	return responseDto{Success: true, Data: lightning.ServiceStatus()}
}

// GetListPayments handles the GET request to list payments.
func (lightning *Lightning) GetListPayments(_ *http.Request) interface{} {
	payments, err := lightning.ListPayments()
	if err != nil {
		return errorResponse(err)
	}
	return responseDto{Success: true, Data: payments}
}

// GetCredentials handles the GET request to retrieve the wallet credentials for the frontend
// wallet engine.
func (lightning *Lightning) GetCredentials(_ *http.Request) interface{} {
	credentials, err := lightning.Credentials()
	if err != nil {
		return errorResponse(err)
	}
	return responseDto{Success: true, Data: credentials}
}

// PostState handles the POST request pushing the current wallet state from the frontend
// wallet engine.
func (lightning *Lightning) PostState(r *http.Request) interface{} {
	var state walletState
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return errorResponse(err)
	}

	lightning.SetState(&state)
	return responseDto{Success: true}
}
