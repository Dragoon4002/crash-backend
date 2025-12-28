#!/bin/bash

# Load environment variables from .env file
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
    echo "✅ Loaded environment variables from .env"
else
    echo "⚠️  Warning: .env file not found"
    echo "Please create .env with OWNER_PRIVATE_KEY=your_key"
    exit 1
fi

# Verify OWNER_PRIVATE_KEY is set
if [ -z "$OWNER_PRIVATE_KEY" ]; then
    echo "❌ Error: OWNER_PRIVATE_KEY not found in .env"
    exit 1
fi

echo "🔑 OWNER_PRIVATE_KEY loaded (${#OWNER_PRIVATE_KEY} characters)"
echo "🚀 Starting Go server..."
echo ""

# Run the server
go run main.go
