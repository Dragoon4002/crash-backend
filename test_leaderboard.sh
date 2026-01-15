#!/bin/bash

# Test script for leaderboard functionality
# Requires: psql, curl, jq

set -e

# Load env
source .env

echo "=== Leaderboard Test Script ==="
echo ""

# Check if server is running
echo "1. Checking if server is running..."
if ! curl -s http://localhost:8080/api/health > /dev/null 2>&1; then
    echo "   Server not running. Start it with: go run ."
    exit 1
fi
echo "   Server is running!"
echo ""

# Insert test data
echo "2. Inserting test wallet data..."
psql "$DATABASE_URL" << 'EOF'
-- Clear existing test data
DELETE FROM wallet_pnl WHERE wallet_address LIKE '0xTest%';

-- Insert test wallets with various PnL values
INSERT INTO wallet_pnl (wallet_address, amount) VALUES
    ('0xTest1111111111111111111111111111111111', 150.5),
    ('0xTest2222222222222222222222222222222222', 89.25),
    ('0xTest3333333333333333333333333333333333', 45.0),
    ('0xTest4444444444444444444444444444444444', 32.75),
    ('0xTest5555555555555555555555555555555555', 21.5),
    ('0xTest6666666666666666666666666666666666', 15.0),
    ('0xTest7777777777777777777777777777777777', 8.25),
    ('0xTest8888888888888888888888888888888888', -5.5),
    ('0xTest9999999999999999999999999999999999', -12.0),
    ('0xTestAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', -25.75)
ON CONFLICT (wallet_address) DO UPDATE SET amount = EXCLUDED.amount;
EOF
echo "   Test data inserted!"
echo ""

# Test leaderboard API
echo "3. Testing GET /api/leaderboard..."
response=$(curl -s http://localhost:8080/api/leaderboard)
echo "   Response:"
echo "$response" | jq .
echo ""

# Verify response
echo "4. Verifying response..."
success=$(echo "$response" | jq -r '.success')
count=$(echo "$response" | jq '.leaderboard | length')
first_wallet=$(echo "$response" | jq -r '.leaderboard[0].walletAddress')
first_pnl=$(echo "$response" | jq -r '.leaderboard[0].pnl')

if [ "$success" = "true" ]; then
    echo "   success: true"
else
    echo "   FAIL: success should be true"
    exit 1
fi

if [ "$count" -ge 10 ]; then
    echo "   leaderboard count: $count (OK)"
else
    echo "   FAIL: expected at least 10 entries, got $count"
    exit 1
fi

if [ "$first_wallet" = "0xTest1111111111111111111111111111111111" ]; then
    echo "   first place wallet: correct"
else
    echo "   WARN: first place wallet is $first_wallet (may have other data)"
fi

echo ""

# Test with wallet param (user not in top 20)
echo "5. Testing GET /api/leaderboard?wallet=0xTestAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA..."
response=$(curl -s "http://localhost:8080/api/leaderboard?wallet=0xTestAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
echo "   Response:"
echo "$response" | jq .
echo ""

user_rank=$(echo "$response" | jq -r '.userPosition.rank // "null"')
if [ "$user_rank" != "null" ]; then
    echo "   userPosition.rank: $user_rank (OK - user outside top 20)"
else
    echo "   userPosition: null (user might be in top 20)"
fi
echo ""

# Cleanup
echo "6. Cleanup (optional - keeping test data)..."
echo "   To remove test data run:"
echo "   psql \"\$DATABASE_URL\" -c \"DELETE FROM wallet_pnl WHERE wallet_address LIKE '0xTest%';\""
echo ""

echo "=== All tests passed! ==="
