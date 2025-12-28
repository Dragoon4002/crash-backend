package contract

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	// Mantle Sepolia RPC
	MantleSepoliaRPC = "https://rpc.sepolia.mantle.xyz"

	// Contract address (GameHouseV2)
	ContractAddress = "0x2A9caFEDFc91d55E00B6d1514E39BeB940832b5D"

	// Chain ID for Mantle Sepolia
	ChainID = 5003
)

// GameHouseContract wraps the contract interaction
type GameHouseContract struct {
	client      *ethclient.Client
	contract    *bind.BoundContract
	abi         abi.ABI
	address     common.Address
	privateKey  *ecdsa.PrivateKey
	fromAddress common.Address
}

// ABIFile structure
type ABIFile struct {
	ABI json.RawMessage `json:"abi"`
}

// NewGameHouseContract creates a new contract instance
func NewGameHouseContract() (*GameHouseContract, error) {
	// Connect to Mantle Sepolia
	client, err := ethclient.Dial(MantleSepoliaRPC)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Mantle Sepolia: %v", err)
	}

	// Load ABI from JSON file
	abiBytes, err := os.ReadFile("contract/GameHouseV2.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read ABI file: %v", err)
	}

	var abiFile ABIFile
	if err := json.Unmarshal(abiBytes, &abiFile); err != nil {
		return nil, fmt.Errorf("failed to parse ABI JSON: %v", err)
	}

	contractABI, err := abi.JSON(strings.NewReader(string(abiFile.ABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse contract ABI: %v", err)
	}

	// Load private key from environment
	privateKeyHex := os.Getenv("OWNER_PRIVATE_KEY")
	if privateKeyHex == "" {
		return nil, fmt.Errorf("OWNER_PRIVATE_KEY environment variable not set")
	}

	// Remove 0x prefix if present
	if strings.HasPrefix(privateKeyHex, "0x") {
		privateKeyHex = privateKeyHex[2:]
	}

	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("failed to get public key")
	}

	fromAddress := crypto.PubkeyToAddress(*publicKeyECDSA)

	contractAddress := common.HexToAddress(ContractAddress)
	contract := bind.NewBoundContract(contractAddress, contractABI, client, client, client)

	log.Printf("✅ Contract client initialized - Address: %s, Owner: %s", ContractAddress, fromAddress.Hex())

	return &GameHouseContract{
		client:      client,
		contract:    contract,
		abi:         contractABI,
		address:     contractAddress,
		privateKey:  privateKey,
		fromAddress: fromAddress,
	}, nil
}

// RugGame marks a crash game as rugged
func (c *GameHouseContract) RugGame(ctx context.Context, gameID *big.Int) (string, error) {
	// Create transactor
	chainIDBig := big.NewInt(ChainID)
	auth, err := bind.NewKeyedTransactorWithChainID(c.privateKey, chainIDBig)
	if err != nil {
		return "", fmt.Errorf("failed to create transactor: %v", err)
	}

	// Get nonce
	nonce, err := c.client.PendingNonceAt(ctx, c.fromAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))

	// Get gas price
	gasPrice, err := c.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}
	auth.GasPrice = gasPrice

	// Estimate gas limit
	input, err := c.abi.Pack("rugGame", gameID)
	if err != nil {
		return "", fmt.Errorf("failed to pack input: %v", err)
	}

	gasLimit, err := c.client.EstimateGas(ctx, ethereum.CallMsg{
		From: c.fromAddress,
		To:   &c.address,
		Data: input,
	})
	if err != nil {
		log.Printf("⚠️  Gas estimation failed, using default: %v", err)
		auth.GasLimit = uint64(300000) // Fallback gas limit
	} else {
		// Add 20% buffer to estimated gas
		auth.GasLimit = gasLimit + (gasLimit * 20 / 100)
		log.Printf("📊 Estimated gas: %d, using: %d", gasLimit, auth.GasLimit)
	}

	log.Printf("🔨 Calling rugGame(gameId=%s)...", gameID.String())

	// Call rugGame
	tx, err := c.contract.Transact(auth, "rugGame", gameID)
	if err != nil {
		return "", fmt.Errorf("failed to call rugGame: %v", err)
	}

	log.Printf("✅ rugGame transaction sent: %s", tx.Hash().Hex())

	// Wait for confirmation
	receipt, err := bind.WaitMined(ctx, c.client, tx)
	if err != nil {
		return "", fmt.Errorf("failed to wait for transaction: %v", err)
	}

	if receipt.Status != 1 {
		return "", fmt.Errorf("transaction failed with status %d", receipt.Status)
	}

	log.Printf("✅ rugGame confirmed in block %d", receipt.BlockNumber.Uint64())

	return tx.Hash().Hex(), nil
}

// ResolveCandleFlip resolves a CandleFlip game
func (c *GameHouseContract) ResolveCandleFlip(ctx context.Context, gameID *big.Int, roomsWon *big.Int) (string, error) {
	// Create transactor
	chainIDBig := big.NewInt(ChainID)
	auth, err := bind.NewKeyedTransactorWithChainID(c.privateKey, chainIDBig)
	if err != nil {
		return "", fmt.Errorf("failed to create transactor: %v", err)
	}

	// Get nonce
	nonce, err := c.client.PendingNonceAt(ctx, c.fromAddress)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %v", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))

	// Get gas price
	gasPrice, err := c.client.SuggestGasPrice(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get gas price: %v", err)
	}
	auth.GasPrice = gasPrice

	// Estimate gas limit
	input, err := c.abi.Pack("resolveCandleFlip", gameID, roomsWon)
	if err != nil {
		return "", fmt.Errorf("failed to pack input: %v", err)
	}

	gasLimit, err := c.client.EstimateGas(ctx, ethereum.CallMsg{
		From: c.fromAddress,
		To:   &c.address,
		Data: input,
	})
	if err != nil {
		log.Printf("⚠️  Gas estimation failed, using default: %v", err)
		auth.GasLimit = uint64(300000) // Fallback gas limit
	} else {
		// Add 20% buffer to estimated gas
		auth.GasLimit = gasLimit + (gasLimit * 20 / 100)
		log.Printf("📊 Estimated gas: %d, using: %d", gasLimit, auth.GasLimit)
	}

	log.Printf("🎲 Calling resolveCandleFlip(gameId=%s, roomsWon=%s)...", gameID.String(), roomsWon.String())

	// Call resolveCandleFlip
	tx, err := c.contract.Transact(auth, "resolveCandleFlip", gameID, roomsWon)
	if err != nil {
		return "", fmt.Errorf("failed to call resolveCandleFlip: %v", err)
	}

	log.Printf("✅ resolveCandleFlip transaction sent: %s", tx.Hash().Hex())

	// Wait for confirmation
	receipt, err := bind.WaitMined(ctx, c.client, tx)
	if err != nil {
		return "", fmt.Errorf("failed to wait for transaction: %v", err)
	}

	if receipt.Status != 1 {
		return "", fmt.Errorf("transaction failed with status %d", receipt.Status)
	}

	log.Printf("✅ resolveCandleFlip confirmed in block %d", receipt.BlockNumber.Uint64())

	return tx.Hash().Hex(), nil
}

// CashOutFor executes a cashout on behalf of a player (gasless transaction)
// Only callable by contract owner (relayer)
func (c *GameHouseContract) CashOutFor(auth *bind.TransactOpts, player common.Address, gameID *big.Int, currentMultiplier *big.Int) (*types.Transaction, error) {
	// Call cashOutFor function on contract
	tx, err := c.contract.Transact(auth, "cashOutFor", player, gameID, currentMultiplier)
	if err != nil {
		return nil, fmt.Errorf("failed to call cashOutFor: %w", err)
	}

	log.Printf("✅ cashOutFor transaction sent for player %s: %s", player.Hex(), tx.Hash().Hex())
	return tx, nil
}

// BuyInFor executes a buy-in on behalf of a player (gasless transaction)
// Only callable by contract owner (relayer)
func (c *GameHouseContract) BuyInFor(auth *bind.TransactOpts, player common.Address, gameID *big.Int, entryMultiplier *big.Int) (*types.Transaction, error) {
	// Call buyInFor function on contract
	tx, err := c.contract.Transact(auth, "buyInFor", player, gameID, entryMultiplier)
	if err != nil {
		return nil, fmt.Errorf("failed to call buyInFor: %w", err)
	}

	log.Printf("✅ buyInFor transaction sent for player %s: %s", player.Hex(), tx.Hash().Hex())
	return tx, nil
}

// Close closes the client connection
func (c *GameHouseContract) Close() {
	c.client.Close()
}
