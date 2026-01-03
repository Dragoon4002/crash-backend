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
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	// Mantle Sepolia RPC
	MantleSepoliaRPC = "https://rpc.sepolia.mantle.xyz"

	// Contract address (GameHouseV3)
	ContractAddress = "0x80Fc067cDDCDE4a78199a7A6751F2f629654b93A"

	// Chain ID for Mantle Sepolia
	ChainID = 5003
)

// GameHouseContract wraps the contract interaction
type GameHouseContract struct {
	Client      *ethclient.Client
	Contract    *bind.BoundContract
	ABI         abi.ABI
	Address     common.Address
	PrivateKey  *ecdsa.PrivateKey
	FromAddress common.Address
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
	abiBytes, err := os.ReadFile("contract/GameHouseNoSig.json")
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
	privateKeyHex := os.Getenv("SERVER_PRIVATE_KEY")
	if privateKeyHex == "" {
		return nil, fmt.Errorf("SERVER_PRIVATE_KEY environment variable not set")
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
		Client:      client,
		Contract:    contract,
		ABI:         contractABI,
		Address:     contractAddress,
		PrivateKey:  privateKey,
		FromAddress: fromAddress,
	}, nil
}

// PayPlayer calls the V3 contract's payPlayer method
// This is the ONLY payment method - server pays gas, no retries
func (c *GameHouseContract) PayPlayer(
	ctx context.Context,
	player common.Address,
	amount *big.Int,
) error {
	// Ensure ABI has the function
	if _, ok := c.ABI.Methods["payPlayer"]; !ok {
		return fmt.Errorf("abi does not contain payPlayer")
	}

	// Create transactor (server pays gas)
	chainIDBig := big.NewInt(ChainID)
	auth, err := bind.NewKeyedTransactorWithChainID(c.PrivateKey, chainIDBig)
	if err != nil {
		return fmt.Errorf("failed to create transactor: %v", err)
	}
	auth.Context = ctx
	auth.Value = big.NewInt(0) // non-payable

	// Nonce
	nonce, err := c.Client.PendingNonceAt(ctx, c.FromAddress)
	if err != nil {
		return fmt.Errorf("failed to get nonce: %v", err)
	}
	auth.Nonce = big.NewInt(int64(nonce))

	// Gas price
	gasPrice, err := c.Client.SuggestGasPrice(ctx)
	if err != nil {
		return fmt.Errorf("failed to get gas price: %v", err)
	}
	auth.GasPrice = gasPrice

	// Pack input for estimation
	input, err := c.ABI.Pack("payPlayer", player, amount)
	if err != nil {
		return fmt.Errorf("failed to pack input: %v", err)
	}

	// Estimate gas limit
	gasLimit, err := c.Client.EstimateGas(ctx, ethereum.CallMsg{
		From: c.FromAddress,
		To:   &c.Address,
		Data: input,
	})
	if err != nil {
		log.Printf("⚠️ Gas estimation failed, using default: %v", err)
		auth.GasLimit = uint64(200000) // safe default
	} else {
		auth.GasLimit = gasLimit + (gasLimit * 20 / 100) // +20% buffer
	}

	log.Printf("💸 Calling payPlayer(player=%s, amount=%s wei) with gasLimit=%d", 
		player.Hex(), amount.String(), auth.GasLimit)

	// Transact - fire and forget, no wait for confirmation
	tx, err := c.Contract.Transact(auth, "payPlayer", player, amount)
	if err != nil {
		// Log failure but don't block game flow
		log.Printf("❌ payPlayer failed: %v", err)
		return err
	}

	log.Printf("📤 payPlayer tx sent: %s (not waiting for confirmation)", tx.Hash().Hex())
	return nil
}

// Close closes the client connection
func (c *GameHouseContract) Close() {
	c.Client.Close()
}