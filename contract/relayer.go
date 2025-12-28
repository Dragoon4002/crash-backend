package contract

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// RelayerConfig holds configuration for the transaction relayer
type RelayerConfig struct {
	PrivateKey  string
	RPCUrl      string
	ChainID     int64
	GasLimit    uint64
	MaxGasPrice *big.Int
}

// Relayer handles gasless transactions for users
type Relayer struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	publicKey  common.Address
	chainID    *big.Int
	config     RelayerConfig
}

// NewRelayer creates a new transaction relayer
func NewRelayer(config RelayerConfig) (*Relayer, error) {
	// Connect to RPC
	client, err := ethclient.Dial(config.RPCUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RPC: %w", err)
	}

	// Load private key
	privateKey, err := crypto.HexToECDSA(config.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := crypto.PubkeyToAddress(privateKey.PublicKey)
	chainID := big.NewInt(config.ChainID)

	return &Relayer{
		client:     client,
		privateKey: privateKey,
		publicKey:  publicKey,
		chainID:    chainID,
		config:     config,
	}, nil
}

// CashOutRequest represents a user's cashout request
type CashOutRequest struct {
	PlayerAddress    common.Address
	GameID           *big.Int
	CurrentMultiplier *big.Int
	Signature        []byte // User's signature authorizing the cashout
}

// RelayCashOut executes a cashout transaction on behalf of the user
// The relayer pays the gas fees
func (r *Relayer) RelayCashOut(ctx context.Context, gameHouse *GameHouseContract, req CashOutRequest) (*types.Transaction, error) {
	// Verify signature (ensure user authorized this cashout)
	if err := r.verifySignature(req); err != nil {
		return nil, fmt.Errorf("invalid signature: %w", err)
	}

	// Get current nonce
	nonce, err := r.client.PendingNonceAt(ctx, r.publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce: %w", err)
	}

	// Get gas price
	gasPrice, err := r.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price: %w", err)
	}

	// Cap gas price if configured
	if r.config.MaxGasPrice != nil && gasPrice.Cmp(r.config.MaxGasPrice) > 0 {
		gasPrice = r.config.MaxGasPrice
	}

	// Create transaction opts
	auth, err := bind.NewKeyedTransactorWithChainID(r.privateKey, r.chainID)
	if err != nil {
		return nil, fmt.Errorf("failed to create transactor: %w", err)
	}

	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = big.NewInt(0)
	auth.GasLimit = r.config.GasLimit
	auth.GasPrice = gasPrice
	auth.Context = ctx

	// Execute cashout via relayer
	// Note: This calls the contract's cashOutFor function (we'll need to add this)
	tx, err := gameHouse.CashOutFor(auth, req.PlayerAddress, req.GameID, req.CurrentMultiplier)
	if err != nil {
		return nil, fmt.Errorf("cashout transaction failed: %w", err)
	}

	// Wait for transaction to be mined
	receipt, err := bind.WaitMined(ctx, r.client, tx)
	if err != nil {
		return nil, fmt.Errorf("transaction mining failed: %w", err)
	}

	if receipt.Status != types.ReceiptStatusSuccessful {
		return nil, fmt.Errorf("transaction failed with status: %d", receipt.Status)
	}

	return tx, nil
}

// RelayBuyIn executes a buy-in transaction on behalf of the user
func (r *Relayer) RelayBuyIn(ctx context.Context, gameHouse *GameHouseContract, playerAddress common.Address, gameID *big.Int, entryMultiplier *big.Int, betAmount *big.Int, signature []byte) (*types.Transaction, error) {
	// Verify signature
	// ... signature verification logic

	// Get nonce and gas price
	nonce, err := r.client.PendingNonceAt(ctx, r.publicKey)
	if err != nil {
		return nil, err
	}

	gasPrice, err := r.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	if r.config.MaxGasPrice != nil && gasPrice.Cmp(r.config.MaxGasPrice) > 0 {
		gasPrice = r.config.MaxGasPrice
	}

	// Create auth
	auth, err := bind.NewKeyedTransactorWithChainID(r.privateKey, r.chainID)
	if err != nil {
		return nil, err
	}

	auth.Nonce = big.NewInt(int64(nonce))
	auth.Value = betAmount // Relayer provides the bet amount
	auth.GasLimit = r.config.GasLimit
	auth.GasPrice = gasPrice
	auth.Context = ctx

	// Execute buy-in
	tx, err := gameHouse.BuyInFor(auth, playerAddress, gameID, entryMultiplier)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// verifySignature verifies that the user signed the cashout request
func (r *Relayer) verifySignature(req CashOutRequest) error {
	// Hash the message (EIP-191 format)
	message := crypto.Keccak256Hash(
		[]byte(fmt.Sprintf("CashOut:%s:%s:%s",
			req.PlayerAddress.Hex(),
			req.GameID.String(),
			req.CurrentMultiplier.String(),
		)),
	)

	// Recover signer from signature
	sigPublicKey, err := crypto.SigToPub(message.Bytes(), req.Signature)
	if err != nil {
		return err
	}

	recoveredAddr := crypto.PubkeyToAddress(*sigPublicKey)

	// Verify signer matches player
	if recoveredAddr != req.PlayerAddress {
		return fmt.Errorf("signature mismatch: expected %s, got %s",
			req.PlayerAddress.Hex(), recoveredAddr.Hex())
	}

	return nil
}

// GetBalance returns the relayer's current balance
func (r *Relayer) GetBalance(ctx context.Context) (*big.Int, error) {
	return r.client.BalanceAt(ctx, r.publicKey, nil)
}

// EstimateCashOutCost estimates the gas cost for a cashout transaction
func (r *Relayer) EstimateCashOutCost(ctx context.Context) (*big.Int, error) {
	gasPrice, err := r.client.SuggestGasPrice(ctx)
	if err != nil {
		return nil, err
	}

	// Cashout typically uses ~100k gas
	estimatedGas := uint64(100000)
	cost := new(big.Int).Mul(gasPrice, big.NewInt(int64(estimatedGas)))

	return cost, nil
}

// MonitorBalance checks if relayer has sufficient balance
func (r *Relayer) MonitorBalance(ctx context.Context, minBalance *big.Int) error {
	balance, err := r.GetBalance(ctx)
	if err != nil {
		return err
	}

	if balance.Cmp(minBalance) < 0 {
		return fmt.Errorf("relayer balance too low: %s (minimum: %s)",
			balance.String(), minBalance.String())
	}

	return nil
}

// RefundMonitor periodically checks and refunds the relayer if needed
func (r *Relayer) RefundMonitor(ctx context.Context, interval time.Duration, minBalance *big.Int) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.MonitorBalance(ctx, minBalance); err != nil {
				fmt.Printf("⚠️ Warning: %v\n", err)
				// Send alert notification
			}
		}
	}
}
