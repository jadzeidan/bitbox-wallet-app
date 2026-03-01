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
	"strconv"
	"strings"
	"sync"
	"time"

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
	mu         sync.Mutex
	nextCallAt time.Time
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
	const maxAttempts = 5
	const baseBackoff = 250 * time.Millisecond
	const maxBackoff = 3 * time.Second
	const minRequestInterval = 120 * time.Millisecond

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := c.pace(ctx, minRequestInterval); err != nil {
			return err
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
			// Network errors can be transient; retry while attempts remain.
			lastErr = errp.WithStack(err)
			if attempt < maxAttempts-1 {
				if err := sleepWithContext(ctx, backoffDelay(baseBackoff, maxBackoff, attempt)); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}

		body, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		response.Body.Close()
		if readErr != nil {
			lastErr = errp.WithStack(readErr)
			if attempt < maxAttempts-1 {
				if err := sleepWithContext(ctx, backoffDelay(baseBackoff, maxBackoff, attempt)); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}

		if response.StatusCode < 200 || response.StatusCode >= 300 {
			lastErr = errp.Newf("solana rpc status %d: %s", response.StatusCode, string(body))
			if response.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts-1 {
				if err := sleepWithContext(ctx, retryDelay(response.Header.Get("Retry-After"), baseBackoff, maxBackoff, attempt)); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}

		var decoded rpcResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			lastErr = errp.WithStack(err)
			if attempt < maxAttempts-1 {
				if err := sleepWithContext(ctx, backoffDelay(baseBackoff, maxBackoff, attempt)); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}
		if decoded.Error != nil {
			lastErr = errp.Newf("solana rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
			if isRateLimitedRPCError(decoded.Error.Code, decoded.Error.Message) && attempt < maxAttempts-1 {
				if err := sleepWithContext(ctx, backoffDelay(baseBackoff, maxBackoff, attempt)); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}
		if result == nil {
			return nil
		}
		if err := json.Unmarshal(decoded.Result, result); err != nil {
			return errp.WithStack(err)
		}
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return errp.New("solana rpc call failed")
}

func (c *RPCClient) pace(ctx context.Context, minInterval time.Duration) error {
	c.mu.Lock()
	now := time.Now()
	wait := c.nextCallAt.Sub(now)
	if wait < 0 {
		wait = 0
	}
	next := now
	if c.nextCallAt.After(now) {
		next = c.nextCallAt
	}
	c.nextCallAt = next.Add(minInterval)
	c.mu.Unlock()
	return sleepWithContext(ctx, wait)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errp.WithStack(ctx.Err())
	case <-timer.C:
		return nil
	}
}

func backoffDelay(base time.Duration, max time.Duration, attempt int) time.Duration {
	d := base * time.Duration(1<<attempt)
	if d > max {
		return max
	}
	return d
}

func retryDelay(retryAfter string, base time.Duration, max time.Duration, attempt int) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil && seconds > 0 {
			d := time.Duration(seconds) * time.Second
			if d > max {
				return max
			}
			return d
		}
	}
	return backoffDelay(base, max, attempt)
}

func isRateLimitedRPCError(code int, message string) bool {
	if code == -32429 {
		return true
	}
	msg := strings.ToLower(message)
	return strings.Contains(msg, "rate limit")
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

// SignatureInfo is a subset of getSignaturesForAddress results.
type SignatureInfo struct {
	Signature          string `json:"signature"`
	Slot               uint64 `json:"slot"`
	BlockTime          *int64 `json:"blockTime"`
	ConfirmationStatus string `json:"confirmationStatus"`
	Err                *struct {
	} `json:"err,omitempty"`
}

// TransactionInfo is a subset of getTransaction results.
type TransactionInfo struct {
	Slot      uint64 `json:"slot"`
	BlockTime *int64 `json:"blockTime"`
	Meta      struct {
		Err         *struct{} `json:"err,omitempty"`
		Fee         uint64    `json:"fee"`
		PreBalances []uint64  `json:"preBalances"`
		PostBalances []uint64 `json:"postBalances"`
	} `json:"meta"`
	Transaction struct {
		Message struct {
			AccountKeys  []transactionAccountKey `json:"accountKeys"`
			Instructions []transactionInstruction `json:"instructions"`
		} `json:"message"`
		Signatures []string `json:"signatures"`
	} `json:"transaction"`
}

type transactionAccountKey struct {
	Pubkey string
}

func (k *transactionAccountKey) UnmarshalJSON(data []byte) error {
	// "json" encoding: account key is a string.
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		k.Pubkey = asString
		return nil
	}
	// "jsonParsed" encoding: account key is an object with a pubkey field.
	var asObject struct {
		Pubkey string `json:"pubkey"`
	}
	if err := json.Unmarshal(data, &asObject); err != nil {
		return err
	}
	k.Pubkey = asObject.Pubkey
	return nil
}

type parsedInstruction struct {
	Type string                 `json:"type"`
	Info map[string]interface{} `json:"info"`
}

type transactionInstruction struct {
	Program string             `json:"program"`
	Parsed  *parsedInstruction `json:"parsed,omitempty"`
}

func (c *RPCClient) GetSignaturesForAddress(
	ctx context.Context,
	address string,
	limit int,
) ([]SignatureInfo, error) {
	if limit <= 0 {
		limit = 20
	}
	var out []SignatureInfo
	if err := c.call(ctx, "getSignaturesForAddress", []interface{}{
		address,
		map[string]interface{}{
			"limit":      limit,
			"commitment": "confirmed",
		},
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *RPCClient) GetTransaction(ctx context.Context, signature string) (*TransactionInfo, error) {
	var out *TransactionInfo
	if err := c.call(ctx, "getTransaction", []interface{}{
		signature,
		map[string]interface{}{
			"encoding":                     "jsonParsed",
			"maxSupportedTransactionVersion": 0,
			"commitment":                   "confirmed",
		},
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}
