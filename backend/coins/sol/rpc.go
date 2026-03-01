// SPDX-License-Identifier: Apache-2.0

package sol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/BitBoxSwiss/bitbox-wallet-app/util/errp"
)

type rpcRequest struct {
	ID      int         `json:"id"`
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// RPCClient is a Solana JSON-RPC client.
type RPCClient struct {
	url        string
	apiKey     string
	httpClient *http.Client
}

func NewRPCClient(url string, apiKey string, httpClient *http.Client) *RPCClient {
	return &RPCClient{url: url, apiKey: apiKey, httpClient: httpClient}
}

func (c *RPCClient) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	payload, err := json.Marshal(rpcRequest{
		ID:      1,
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return errp.WithStack(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return errp.WithStack(err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		request.Header.Set("x-api-key", c.apiKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return errp.WithStack(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return errp.Newf("solana rpc status %d: %s", response.StatusCode, string(body))
	}
	var decoded rpcResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&decoded); err != nil {
		return errp.WithStack(err)
	}
	if decoded.Error != nil {
		return errp.Newf("solana rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(decoded.Result, result); err != nil {
		return errp.WithStack(err)
	}
	return nil
}

func (c *RPCClient) GetBalance(ctx context.Context, address string) (uint64, error) {
	var out struct {
		Value uint64 `json:"value"`
	}
	if err := c.call(ctx, "getBalance", []interface{}{address, map[string]interface{}{"commitment": "confirmed"}}, &out); err != nil {
		return 0, err
	}
	return out.Value, nil
}

func (c *RPCClient) GetLatestBlockhash(ctx context.Context) (string, error) {
	var out struct {
		Value struct {
			Blockhash string `json:"blockhash"`
		} `json:"value"`
	}
	if err := c.call(ctx, "getLatestBlockhash", []interface{}{map[string]interface{}{"commitment": "confirmed"}}, &out); err != nil {
		return "", err
	}
	if out.Value.Blockhash == "" {
		return "", errp.New("missing blockhash in getLatestBlockhash response")
	}
	return out.Value.Blockhash, nil
}

func (c *RPCClient) GetFeeForMessage(ctx context.Context, message []byte) (uint64, error) {
	messageB64 := base64.StdEncoding.EncodeToString(message)
	var out struct {
		Value *uint64 `json:"value"`
	}
	if err := c.call(ctx, "getFeeForMessage", []interface{}{messageB64, map[string]interface{}{"commitment": "confirmed"}}, &out); err != nil {
		return 0, err
	}
	if out.Value == nil {
		return 0, errp.New("missing fee value")
	}
	return *out.Value, nil
}

func (c *RPCClient) SendTransaction(ctx context.Context, tx []byte) (string, error) {
	txB64 := base64.StdEncoding.EncodeToString(tx)
	var signature string
	params := []interface{}{txB64, map[string]interface{}{"encoding": "base64", "skipPreflight": false}}
	if err := c.call(ctx, "sendTransaction", params, &signature); err != nil {
		return "", err
	}
	if signature == "" {
		return "", fmt.Errorf("empty signature")
	}
	return signature, nil
}
