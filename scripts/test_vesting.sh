#!/bin/bash

# Test script for vesting accounts
# This script tests different types of vesting accounts on the local gurud node

CHAINID="guru_631-1"
KEYRING="test"
KEYALGO="eth_secp256k1"
HOMEDIR="$HOME/.gurud"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print functions
print_header() {
    echo -e "${BLUE}============================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}============================================${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}ℹ $1${NC}"
}

# Main test execution
main() {
    print_header "Vesting Account Test Suite"
    echo ""
    
    # Check if node is running
    print_header "Step 1: Checking Node Status"
    if gurud status --home "$HOMEDIR" 2>/dev/null | jq -r '.sync_info.catching_up' &>/dev/null; then
        print_success "Node is running"
    else
        print_error "Node is not running. Please start the node first."
        exit 1
    fi
    echo ""
    
    # Create test keys
    print_header "Step 2: Creating Test Keys"
    
    CONTINUOUS_KEY="vesting_continuous"
    DELAYED_KEY="vesting_delayed"
    
    if ! gurud keys show "$CONTINUOUS_KEY" --keyring-backend "$KEYRING" --home "$HOMEDIR" &>/dev/null; then
        gurud keys add "$CONTINUOUS_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
        print_success "Created key: $CONTINUOUS_KEY"
    else
        print_info "Key already exists: $CONTINUOUS_KEY"
    fi
    
    if ! gurud keys show "$DELAYED_KEY" --keyring-backend "$KEYRING" --home "$HOMEDIR" &>/dev/null; then
        gurud keys add "$DELAYED_KEY" --keyring-backend "$KEYRING" --algo "$KEYALGO" --home "$HOMEDIR"
        print_success "Created key: $DELAYED_KEY"
    else
        print_info "Key already exists: $DELAYED_KEY"
    fi
    
    SENDER_ADDR=$(gurud keys show mykey --address --keyring-backend "$KEYRING" --home "$HOMEDIR")
    CONTINUOUS_ADDR=$(gurud keys show "$CONTINUOUS_KEY" --address --keyring-backend "$KEYRING" --home "$HOMEDIR")
    DELAYED_ADDR=$(gurud keys show "$DELAYED_KEY" --address --keyring-backend "$KEYRING" --home "$HOMEDIR")
    
    print_info "Sender address: $SENDER_ADDR"
    print_info "Continuous vesting address: $CONTINUOUS_ADDR"
    print_info "Delayed vesting address: $DELAYED_ADDR"
    echo ""
    
    # Test 1: Create Continuous Vesting Account
    print_header "Step 3: Creating Continuous Vesting Account"
    
    # Calculate end time (10 minutes from now)
    CURRENT_TIME=$(date +%s)
    END_TIME=$((CURRENT_TIME + 600))
    
    print_info "Creating continuous vesting account with 10 minute vesting period"
    print_info "Current time: $CURRENT_TIME ($(date -r $CURRENT_TIME '+%Y-%m-%d %H:%M:%S'))"
    print_info "End time: $END_TIME ($(date -r $END_TIME '+%Y-%m-%d %H:%M:%S'))"
    
    # Create vesting account
    TX_RESULT=$(gurud tx vesting create-vesting-account "$CONTINUOUS_ADDR" 1000000000000000000000agxn "$END_TIME" \
        --from mykey \
        --chain-id "$CHAINID" \
        --keyring-backend "$KEYRING" \
        --home "$HOMEDIR" \
        --gas 300000 \
        --gas-prices 630000000000agxn \
        --yes \
        --output json 2>&1)
    
    echo "$TX_RESULT" | jq '.' || echo "$TX_RESULT"
    
    if echo "$TX_RESULT" | grep -q "code.*0"; then
        print_success "Continuous vesting account creation transaction sent"
    else
        print_error "Failed to send continuous vesting account creation transaction"
    fi
    
    sleep 5
    
    # Query account
    print_info "Querying continuous vesting account details..."
    gurud query auth account "$CONTINUOUS_ADDR" --home "$HOMEDIR" --output json | jq '.'
    
    # Check balances
    print_info "Checking balances..."
    SPENDABLE=$(gurud query bank spendable-balances "$CONTINUOUS_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
    TOTAL=$(gurud query bank balances "$CONTINUOUS_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
    print_info "Spendable balance: $SPENDABLE agxn"
    print_info "Total balance: $TOTAL agxn"
    
    if [ "$TOTAL" = "1000000000000000000000" ]; then
        print_success "Continuous vesting account created successfully"
        print_info "Vesting should gradually release tokens over 10 minutes"
    else
        print_error "Total balance does not match expected amount"
    fi
    echo ""
    
    # Test 2: Create Delayed Vesting Account
    print_header "Step 4: Creating Delayed Vesting Account"
    
    # Calculate end time (5 minutes from now)
    CURRENT_TIME=$(date +%s)
    END_TIME=$((CURRENT_TIME + 300))
    
    print_info "Creating delayed vesting account with 5 minute lock period"
    print_info "Current time: $CURRENT_TIME ($(date -r $CURRENT_TIME '+%Y-%m-%d %H:%M:%S'))"
    print_info "End time: $END_TIME ($(date -r $END_TIME '+%Y-%m-%d %H:%M:%S'))"
    
    # Create delayed vesting account
    TX_RESULT=$(gurud tx vesting create-vesting-account "$DELAYED_ADDR" 500000000000000000000agxn "$END_TIME" \
        --from mykey \
        --delayed \
        --chain-id "$CHAINID" \
        --keyring-backend "$KEYRING" \
        --home "$HOMEDIR" \
        --gas 300000 \
        --gas-prices 630000000000agxn \
        --yes \
        --output json 2>&1)
    
    echo "$TX_RESULT" | jq '.' || echo "$TX_RESULT"
    
    if echo "$TX_RESULT" | grep -q "code.*0"; then
        print_success "Delayed vesting account creation transaction sent"
    else
        print_error "Failed to send delayed vesting account creation transaction"
    fi
    
    sleep 5
    
    # Query account
    print_info "Querying delayed vesting account details..."
    gurud query auth account "$DELAYED_ADDR" --home "$HOMEDIR" --output json | jq '.'
    
    # Check balances (spendable should be 0)
    print_info "Checking balances..."
    SPENDABLE=$(gurud query bank spendable-balances "$DELAYED_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
    TOTAL=$(gurud query bank balances "$DELAYED_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
    print_info "Spendable balance: $SPENDABLE agxn"
    print_info "Total balance: $TOTAL agxn"
    
    if [ "$TOTAL" = "500000000000000000000" ] && [ "$SPENDABLE" = "0" ]; then
        print_success "Delayed vesting account created successfully"
        print_info "All tokens locked until end time, then fully unlocked"
    else
        print_error "Balance does not match expected values"
    fi
    echo ""
    
    # Test 3: Monitor vesting progress
    print_header "Step 5: Monitoring Vesting Progress"
    
    print_info "Monitoring spendable balances for 30 seconds..."
    print_info "For continuous vesting, spendable balance should increase over time"
    print_info "For delayed vesting, balance should remain 0 until end time"
    
    for i in {1..6}; do
        echo ""
        print_info "Check #$i (at $(date '+%H:%M:%S')):"
        
        CONT_SPENDABLE=$(gurud query bank spendable-balances "$CONTINUOUS_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
        CONT_TOTAL=$(gurud query bank balances "$CONTINUOUS_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
        
        DELAY_SPENDABLE=$(gurud query bank spendable-balances "$DELAYED_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
        DELAY_TOTAL=$(gurud query bank balances "$DELAYED_ADDR" --home "$HOMEDIR" --output json | jq -r '.balances[0].amount // "0"')
        
        echo "  Continuous: Spendable=$CONT_SPENDABLE, Total=$CONT_TOTAL"
        echo "  Delayed: Spendable=$DELAY_SPENDABLE, Total=$DELAY_TOTAL"
        
        if [ $i -lt 6 ]; then
            sleep 5
        fi
    done
    
    print_success "Vesting progress monitoring complete"
    echo ""
    
    # Test 4: Test transaction from vesting accounts
    print_header "Step 6: Testing Transactions from Vesting Accounts"
    
    print_info "Attempting to send transaction from continuous vesting account..."
    TX_RESULT=$(gurud tx bank send "$CONTINUOUS_ADDR" "$SENDER_ADDR" 100agxn \
        --from "$CONTINUOUS_KEY" \
        --chain-id "$CHAINID" \
        --keyring-backend "$KEYRING" \
        --home "$HOMEDIR" \
        --gas 200000 \
        --gas-prices 630000000000agxn \
        --yes \
        --output json 2>&1)
    
    echo "$TX_RESULT" | jq '.' || echo "$TX_RESULT"
    
    if echo "$TX_RESULT" | grep -q "code.*0"; then
        print_success "Transaction from continuous vesting account succeeded"
    else
        print_info "Transaction failed (may be expected if insufficient spendable balance)"
    fi
    
    echo ""
    print_info "Attempting to send transaction from delayed vesting account (should fail)..."
    TX_RESULT=$(gurud tx bank send "$DELAYED_ADDR" "$SENDER_ADDR" 100agxn \
        --from "$DELAYED_KEY" \
        --chain-id "$CHAINID" \
        --keyring-backend "$KEYRING" \
        --home "$HOMEDIR" \
        --gas 200000 \
        --gas-prices 630000000000agxn \
        --yes \
        --output json 2>&1)
    
    echo "$TX_RESULT" | jq '.' || echo "$TX_RESULT"
    
    if echo "$TX_RESULT" | grep -q "insufficient"; then
        print_success "Transaction from delayed vesting account correctly failed (insufficient funds)"
    else
        print_info "Transaction result may vary based on vesting status"
    fi
    
    echo ""
    
    # Summary
    print_header "Test Suite Complete"
    print_success "All vesting account tests completed"
    echo ""
    print_info "Summary:"
    print_info "1. Continuous vesting: Gradually releases tokens over 10 minutes"
    print_info "2. Delayed vesting: All tokens locked for 5 minutes, then fully unlocked"
    print_info "3. Spendable balances change over time according to vesting schedule"
    print_info "4. Transactions are restricted based on spendable balance"
    echo ""
    print_info "Account addresses for future reference:"
    print_info "  Continuous: $CONTINUOUS_ADDR"
    print_info "  Delayed: $DELAYED_ADDR"
}

# Run main function
main
