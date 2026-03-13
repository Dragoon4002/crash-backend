package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

const (
	HorizonURL        = "https://horizon.stellar.org"
	DefaultRPCURL     = "https://mainnet.sorobanrpc.com"
	NetworkPassphrase = network.PublicNetworkPassphrase
)

// GameHouseContract wraps the Soroban crash-house contract
type GameHouseContract struct {
	ContractID string
	ServerKP   *keypair.Full
	RpcURL     string
}

// NewGameHouseContract reads env vars and returns a ready client
func NewGameHouseContract() (*GameHouseContract, error) {
	contractID := os.Getenv("CONTRACT_ID")
	serverSecret := os.Getenv("SERVER_PRIVATE_KEY")
	rpcURL := os.Getenv("RPC_URL")

	if contractID == "" {
		return nil, fmt.Errorf("CONTRACT_ID env var required")
	}
	if serverSecret == "" {
		return nil, fmt.Errorf("SERVER_PRIVATE_KEY env var required")
	}
	if rpcURL == "" {
		rpcURL = DefaultRPCURL
	}

	kp, err := keypair.ParseFull(serverSecret)
	if err != nil {
		return nil, fmt.Errorf("invalid SERVER_PRIVATE_KEY: %v", err)
	}

	log.Printf("✅ Stellar contract client initialized - Contract: %s, Server: %s", contractID, kp.Address())

	return &GameHouseContract{
		ContractID: contractID,
		ServerKP:   kp,
		RpcURL:     rpcURL,
	}, nil
}

// PayPlayer calls pay_player on the Soroban crash-house contract.
// playerAddress is a Stellar G... address.
// amount is in stroops (1 XLM = 10_000_000 stroops).
func (c *GameHouseContract) PayPlayer(ctx context.Context, playerAddress string, amount *big.Int) error {
	// 1. Get server account sequence number
	seqNum, err := c.getAccountSequence(ctx, c.ServerKP.Address())
	if err != nil {
		return fmt.Errorf("get sequence: %v", err)
	}

	// 2. Build InvokeHostFunction operation
	invokeOp, err := c.buildPayPlayerOp(playerAddress, amount)
	if err != nil {
		return fmt.Errorf("build op: %v", err)
	}

	// 3. Build initial transaction (fee and soroban data updated after simulation)
	sourceAccount := txnbuild.SimpleAccount{
		AccountID: c.ServerKP.Address(),
		Sequence:  seqNum,
	}

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{invokeOp},
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(30)},
	})
	if err != nil {
		return fmt.Errorf("build tx: %v", err)
	}

	// 4. Serialize for simulation
	txBase64, err := tx.Base64()
	if err != nil {
		return fmt.Errorf("serialize tx: %v", err)
	}

	// 5. Simulate to get soroban data + resource fee
	simResult, err := c.simulateTransaction(ctx, txBase64)
	if err != nil {
		return fmt.Errorf("simulate: %v", err)
	}

	// 6. Apply soroban data and updated fee directly on the XDR envelope
	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(simResult.TransactionData, &sorobanData); err != nil {
		return fmt.Errorf("parse soroban data: %v", err)
	}

	minFee, _ := strconv.ParseInt(simResult.MinResourceFee, 10, 64)
	totalFee := int64(txnbuild.MinBaseFee) + minFee

	// Modify the raw XDR envelope
	env := tx.ToXDR()
	env.V1.Tx.Ext = xdr.TransactionExt{V: 1, SorobanData: &sorobanData}
	env.V1.Tx.Fee = xdr.Uint32(totalFee)

	// Reconstruct Transaction from modified envelope
	envBase64, err := xdr.MarshalBase64(env)
	if err != nil {
		return fmt.Errorf("marshal modified env: %v", err)
	}
	genericTx, err := txnbuild.TransactionFromXDR(envBase64)
	if err != nil {
		return fmt.Errorf("parse modified env: %v", err)
	}
	tx, ok := genericTx.Transaction()
	if !ok {
		return fmt.Errorf("expected a regular transaction")
	}

	// 7. Sign with server key
	tx, err = tx.Sign(NetworkPassphrase, c.ServerKP)
	if err != nil {
		return fmt.Errorf("sign: %v", err)
	}

	// 8. Submit
	signedBase64, err := tx.Base64()
	if err != nil {
		return fmt.Errorf("serialize signed tx: %v", err)
	}

	sendResult, err := c.sendTransaction(ctx, signedBase64)
	if err != nil {
		return fmt.Errorf("send tx: %v", err)
	}
	if sendResult.Status == "ERROR" {
		return fmt.Errorf("tx rejected: %s", sendResult.ErrorResultXDR)
	}

	log.Printf("📤 pay_player submitted - Player: %s, Amount: %s stroops, Hash: %s",
		playerAddress, amount.String(), sendResult.Hash)

	// 9. Poll for confirmation (max 30s, non-blocking on timeout)
	for i := 0; i < 30; i++ {
		getTxR, err := c.getTransaction(ctx, sendResult.Hash)
		if err == nil {
			switch getTxR.Status {
			case "SUCCESS":
				log.Printf("✅ pay_player confirmed - Player: %s, Hash: %s", playerAddress, sendResult.Hash)
				return nil
			case "FAILED":
				return fmt.Errorf("tx failed: %s", getTxR.ResultXDR)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	// Don't block game flow if not confirmed within 30s
	log.Printf("⚠️  pay_player not confirmed in 30s - Hash: %s (continuing)", sendResult.Hash)
	return nil
}

// Close is a no-op (no persistent connection to close)
func (c *GameHouseContract) Close() {}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (c *GameHouseContract) buildPayPlayerOp(playerAddress string, amount *big.Int) (*txnbuild.InvokeHostFunction, error) {
	// Decode contract ID (C... Stellar address) → 32-byte ContractId
	contractBytes, err := strkey.Decode(strkey.VersionByteContract, c.ContractID)
	if err != nil {
		return nil, fmt.Errorf("decode contract id: %v", err)
	}
	var contractID xdr.ContractId
	copy(contractID[:], contractBytes)

	// Player address (G... Stellar account) → ScVal Address
	playerAccountID := xdr.AccountId{}
	if err := playerAccountID.SetAddress(playerAddress); err != nil {
		return nil, fmt.Errorf("parse player address: %v", err)
	}
	playerScVal := xdr.ScVal{
		Type: xdr.ScValTypeScvAddress,
		Address: &xdr.ScAddress{
			Type:      xdr.ScAddressTypeScAddressTypeAccount,
			AccountId: &playerAccountID,
		},
	}

	// Amount as i128 ScVal (stroops fit in 64-bit lo, hi = 0)
	amountScVal := xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{
			Hi: xdr.Int64(0),
			Lo: xdr.Uint64(amount.Uint64()),
		},
	}

	return &txnbuild.InvokeHostFunction{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: xdr.ScAddress{
					Type:       xdr.ScAddressTypeScAddressTypeContract,
					ContractId: &contractID,
				},
				FunctionName: "pay_player",
				Args:         xdr.ScVec{playerScVal, amountScVal},
			},
		},
	}, nil
}

// getAccountSequence fetches the current sequence number from Horizon
func (c *GameHouseContract) getAccountSequence(ctx context.Context, address string) (int64, error) {
	url := fmt.Sprintf("%s/accounts/%s", HorizonURL, address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var account struct {
		Sequence string `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return 0, err
	}
	var seq int64
	fmt.Sscan(account.Sequence, &seq)
	return seq, nil
}

// ── Soroban RPC JSON-RPC client ───────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type simulateResult struct {
	TransactionData string `json:"transactionData"`
	MinResourceFee  string `json:"minResourceFee"`
	Error           string `json:"error,omitempty"`
}

type sendTxResult struct {
	Hash           string `json:"hash"`
	Status         string `json:"status"`
	ErrorResultXDR string `json:"errorResultXdr,omitempty"`
}

type getTxResult struct {
	Status    string `json:"status"`
	ResultXDR string `json:"resultXdr,omitempty"`
}

func (c *GameHouseContract) rpcCall(ctx context.Context, method string, params interface{}, result interface{}) error {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RpcURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return err
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("rpc %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return json.Unmarshal(rpcResp.Result, result)
}

func (c *GameHouseContract) simulateTransaction(ctx context.Context, txXDR string) (*simulateResult, error) {
	var result simulateResult
	if err := c.rpcCall(ctx, "simulateTransaction", map[string]string{"transaction": txXDR}, &result); err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, fmt.Errorf("simulation error: %s", result.Error)
	}
	return &result, nil
}

func (c *GameHouseContract) sendTransaction(ctx context.Context, txXDR string) (*sendTxResult, error) {
	var result sendTxResult
	err := c.rpcCall(ctx, "sendTransaction", map[string]string{"transaction": txXDR}, &result)
	return &result, err
}

func (c *GameHouseContract) getTransaction(ctx context.Context, hash string) (*getTxResult, error) {
	var result getTxResult
	err := c.rpcCall(ctx, "getTransaction", map[string]string{"hash": hash}, &result)
	return &result, err
}
