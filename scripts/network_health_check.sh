#!/bin/bash

# Guru Chain Network Health Check Script
# 네트워크 런칭 후 자동 체크 스크립트

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
GURUD_HOME="${GURUD_HOME:-$HOME/.gurud}"
CHAIN_ID="${CHAIN_ID:-guru_631-1}"
NETWORK_HOST="${NETWORK_HOST:-localhost}"
RPC_PORT="${RPC_PORT:-26657}"
JSON_RPC_PORT="${JSON_RPC_PORT:-8545}"
GRPC_PORT="${GRPC_PORT:-9090}"
API_PORT="${API_PORT:-1317}"
METRICS_PORT="${METRICS_PORT:-26660}"
EVM_METRICS_PORT="${EVM_METRICS_PORT:-6065}"

# Test results
PASSED_TESTS=0
FAILED_TESTS=0
TOTAL_TESTS=0

# Helper functions
print_header() {
    echo -e "\n${BLUE}=== $1 ===${NC}"
}

print_test() {
    echo -e "${YELLOW}Testing: $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ PASS: $1${NC}"
    ((PASSED_TESTS++))
    ((TOTAL_TESTS++))
}

print_failure() {
    echo -e "${RED}✗ FAIL: $1${NC}"
    if [ -n "$2" ]; then
        echo -e "${RED}  Error: $2${NC}"
    fi
    ((FAILED_TESTS++))
    ((TOTAL_TESTS++))
}

print_warning() {
    echo -e "${YELLOW}⚠ WARNING: $1${NC}"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check if port is open
check_port() {
    local port=$1
    if nc -z $NETWORK_HOST $port 2>/dev/null; then
        return 0
    else
        return 1
    fi
}

# Make HTTP request with timeout
make_request() {
    local url=$1
    local timeout=${2:-10}
    curl -s --max-time $timeout "$url" 2>/dev/null
}

# Make HTTP/HTTPS request with automatic protocol detection
make_smart_request() {
    local path=$1
    local timeout=${2:-10}
    
    if [ "$NETWORK_HOST" = "localhost" ]; then
        curl -s --max-time $timeout "http://$NETWORK_HOST$path" 2>/dev/null
    else
        # Try HTTPS first for remote hosts
        local response=$(curl -s --max-time $timeout "https://$NETWORK_HOST$path" 2>/dev/null)
        if [ -n "$response" ] && [[ "$response" != *"400 Bad Request"* ]]; then
            echo "$response"
        else
            # Fallback to HTTP
            curl -s --max-time $timeout "http://$NETWORK_HOST$path" 2>/dev/null
        fi
    fi
}

# Make JSON-RPC request
make_jsonrpc_request() {
    local method=$1
    local params=${2:-"[]"}
    local data="{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":$params,\"id\":1}"
    
    if [ "$NETWORK_HOST" = "localhost" ]; then
        curl -s --max-time 10 -X POST -H "Content-Type: application/json" \
            --data "$data" "http://$NETWORK_HOST:$JSON_RPC_PORT" 2>/dev/null
    else
        # Try HTTPS first for remote hosts
        local response=$(curl -s --max-time 10 -X POST -H "Content-Type: application/json" \
            --data "$data" "https://$NETWORK_HOST:$JSON_RPC_PORT" 2>/dev/null)
        if [ -n "$response" ] && [[ "$response" != *"400 Bad Request"* ]]; then
            echo "$response"
        else
            # Fallback to HTTP
            curl -s --max-time 10 -X POST -H "Content-Type: application/json" \
                --data "$data" "http://$NETWORK_HOST:$JSON_RPC_PORT" 2>/dev/null
        fi
    fi
}

# Check prerequisites
check_prerequisites() {
    print_header "Prerequisites Check"
    
    print_test "Checking required commands"
    local missing_commands=()
    
    for cmd in gurud curl jq nc; do
        if ! command_exists "$cmd"; then
            missing_commands+=("$cmd")
        fi
    done
    
    if [ ${#missing_commands[@]} -eq 0 ]; then
        print_success "All required commands available"
    else
        print_failure "Missing commands: ${missing_commands[*]}"
        echo -e "${RED}Please install missing commands before running this script${NC}"
        exit 1
    fi
}

# 1. Basic Network Health Check
check_network_health() {
    print_header "1. Basic Network Health Check"
    
    # 1.1 Node Status
    print_test "Node process status"
    if pgrep -f "gurud.*start" > /dev/null; then
        print_success "gurud process is running"
    else
        print_failure "gurud process not found"
    fi
    
    # Check if RPC port is accessible
    print_test "CometBFT RPC port ($RPC_PORT)"
    if check_port $RPC_PORT; then
        print_success "CometBFT RPC port is accessible"
    else
        print_failure "CometBFT RPC port is not accessible"
        return
    fi
    
    # Block production
    print_test "Block production"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local status_output
        status_output=$(gurud status 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$status_output" ]; then
            local latest_height=$(echo "$status_output" | jq -r '.sync_info.latest_block_height // "0"')
            if [ "$latest_height" != "0" ] && [ "$latest_height" != "null" ]; then
                print_success "Block height: $latest_height"
                
                # Check if node is syncing
                local catching_up=$(echo "$status_output" | jq -r '.sync_info.catching_up')
                if [ "$catching_up" = "false" ]; then
                    print_success "Node is fully synced"
                else
                    print_warning "Node is still catching up"
                fi
            else
                print_failure "No blocks produced or invalid height"
            fi
        else
            print_failure "Cannot get node status"
        fi
    else
        # Use RPC endpoint for remote hosts
        local status_response=$(make_smart_request ":$RPC_PORT/status")
        if [ -n "$status_response" ]; then
            local latest_height=$(echo "$status_response" | jq -r '.result.sync_info.latest_block_height // "0"')
            if [ "$latest_height" != "0" ] && [ "$latest_height" != "null" ]; then
                print_success "Block height: $latest_height"
                
                # Check if node is syncing
                local catching_up=$(echo "$status_response" | jq -r '.result.sync_info.catching_up')
                if [ "$catching_up" = "false" ]; then
                    print_success "Node is fully synced"
                else
                    print_warning "Node is still catching up"
                fi
            else
                print_failure "No blocks produced or invalid height"
            fi
        else
            print_failure "Cannot get node status via RPC"
        fi
    fi
    
    # P2P connections
    print_test "P2P connections"
    local net_info=$(make_smart_request ":$RPC_PORT/net_info")
    if [ -n "$net_info" ]; then
        local peer_count=$(echo "$net_info" | jq -r '.result.n_peers // "0"')
        if [ "$peer_count" -gt 0 ]; then
            print_success "Connected to $peer_count peers"
        else
            print_warning "No peers connected"
        fi
    else
        print_failure "Cannot get network info"
    fi
    
    # Validator status
    print_test "Validator status"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local validators
        validators=$(gurud query staking validators --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$validators" ]; then
            local validator_count=$(echo "$validators" | jq '.validators | length')
            local active_count=$(echo "$validators" | jq '[.validators[] | select(.status == "BOND_STATUS_BONDED")] | length')
            print_success "Total validators: $validator_count, Active: $active_count"
        else
            print_failure "Cannot query validators"
        fi
    else
        # Use REST API for remote hosts
        local validators=$(make_smart_request ":$API_PORT/cosmos/staking/v1beta1/validators")
        if [ -n "$validators" ]; then
            local validator_count=$(echo "$validators" | jq '.validators | length')
            local active_count=$(echo "$validators" | jq '[.validators[] | select(.status == "BOND_STATUS_BONDED")] | length')
            print_success "Total validators: $validator_count, Active: $active_count"
        else
            print_warning "Cannot query validators via REST API"
        fi
    fi
}

# 2. EVM Compatibility Check
check_evm_compatibility() {
    print_header "2. EVM Compatibility Check"
    
    # JSON-RPC port
    print_test "JSON-RPC port ($JSON_RPC_PORT)"
    if check_port $JSON_RPC_PORT; then
        print_success "JSON-RPC port is accessible"
    else
        print_failure "JSON-RPC port is not accessible"
        return
    fi
    
    # Chain ID check
    print_test "EVM Chain ID"
    local chain_id_response=$(make_jsonrpc_request "eth_chainId")
    if [ -n "$chain_id_response" ]; then
        local chain_id_hex=$(echo "$chain_id_response" | jq -r '.result // "null"')
        if [ "$chain_id_hex" != "null" ]; then
            local chain_id_dec=$((chain_id_hex))
            if [ "$chain_id_dec" = "631" ]; then
                print_success "Chain ID: $chain_id_dec (0x277)"
            else
                print_failure "Unexpected Chain ID: $chain_id_dec"
            fi
        else
            print_failure "Invalid chain ID response"
        fi
    else
        print_failure "Cannot get chain ID"
    fi
    
    # Web3 client version
    print_test "Web3 client version"
    local version_response=$(make_jsonrpc_request "web3_clientVersion")
    if [ -n "$version_response" ]; then
        local version=$(echo "$version_response" | jq -r '.result // "null"')
        if [ "$version" != "null" ]; then
            print_success "Client version: $version"
        else
            print_failure "Invalid client version response"
        fi
    else
        print_failure "Cannot get client version"
    fi
    
    # Latest block
    print_test "Latest block via JSON-RPC"
    local block_response=$(make_jsonrpc_request "eth_blockNumber")
    if [ -n "$block_response" ]; then
        local block_hex=$(echo "$block_response" | jq -r '.result // "null"')
        if [ "$block_hex" != "null" ]; then
            local block_dec=$((block_hex))
            print_success "Latest block: $block_dec"
        else
            print_failure "Invalid block number response"
        fi
    else
        print_failure "Cannot get latest block"
    fi
    
    # EVM parameters
    print_test "EVM module parameters"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local evm_params
        evm_params=$(gurud query evm params --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$evm_params" ]; then
            local evm_denom=$(echo "$evm_params" | jq -r '.params.evm_denom // "null"')
            local enable_create=$(echo "$evm_params" | jq -r '.params.enable_create // "null"')
            local enable_call=$(echo "$evm_params" | jq -r '.params.enable_call // "null"')
            print_success "EVM denom: $evm_denom, Create: $enable_create, Call: $enable_call"
        else
            print_failure "Cannot query EVM parameters"
        fi
    else
        # Use REST API for remote hosts
        local evm_params=$(make_smart_request ":$API_PORT/ethermint/evm/v1/params")
        if [ -n "$evm_params" ]; then
            local evm_denom=$(echo "$evm_params" | jq -r '.params.evm_denom // "null"')
            local enable_create=$(echo "$evm_params" | jq -r '.params.enable_create // "null"')
            local enable_call=$(echo "$evm_params" | jq -r '.params.enable_call // "null"')
            print_success "EVM denom: $evm_denom, Create: $enable_create, Call: $enable_call"
        else
            print_warning "Cannot query EVM parameters via REST API"
        fi
    fi
}

# 3. Custom Modules Check
check_custom_modules() {
    print_header "3. Custom Modules Check"
    
    # ERC20 module
    print_test "ERC20 module parameters"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local erc20_params
        erc20_params=$(gurud query erc20 params --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$erc20_params" ]; then
            print_success "ERC20 module is accessible"
            
            # Token pairs
            local token_pairs
            token_pairs=$(gurud query erc20 token-pairs --output json 2>/dev/null)
            if [ $? -eq 0 ] && [ -n "$token_pairs" ]; then
                local pair_count=$(echo "$token_pairs" | jq '.token_pairs | length')
                print_success "Token pairs: $pair_count"
            else
                print_warning "Cannot query token pairs"
            fi
        else
            print_failure "Cannot query ERC20 parameters"
        fi
    else
        # Use REST API for remote hosts
        print_warning "ERC20 module check not implemented for remote hosts"
    fi
    
    # Fee Market module
    print_test "Fee Market module"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local feemarket_params
        feemarket_params=$(gurud query feemarket params --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$feemarket_params" ]; then
            local base_fee
            base_fee=$(gurud query feemarket base-fee --output json 2>/dev/null | jq -r '.base_fee // "null"')
            print_success "Fee Market module accessible, Base fee: $base_fee"
        else
            print_failure "Cannot query Fee Market parameters"
        fi
    else
        # Use REST API for remote hosts
        print_warning "Fee Market module check not implemented for remote hosts"
    fi
    
    # Oracle module
    print_test "Oracle module"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local oracle_params
        oracle_params=$(gurud query oracle params --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$oracle_params" ]; then
            print_success "Oracle module is accessible"
        else
            print_failure "Cannot query Oracle parameters"
        fi
    else
        # Use REST API for remote hosts
        print_warning "Oracle module check not implemented for remote hosts"
    fi
    
    # Precise Bank module
    print_test "Precise Bank module"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local pb_supply
        pb_supply=$(gurud query precisebank remainder --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$pb_supply" ]; then
            print_success "Precise Bank module is accessible"
        else
            print_failure "Cannot query Precise Bank"
        fi
    else
        # Use REST API for remote hosts
        print_warning "Precise Bank module check not implemented for remote hosts"
    fi
    
    # Fee Policy module
    print_test "FeePolicy module"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local fp_params
        fp_params=$(gurud query feepolicy discounts --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$fp_params" ]; then
            print_success "FeePolicy module is accessible"
        else
            print_failure "Cannot query FeePolicy discounts"
        fi
    else
        # Use REST API for remote hosts
        print_warning "Fee Policy module check not implemented for remote hosts"
    fi
}

# 4. Precompiled Contracts Check
check_precompiled_contracts() {
    print_header "4. Precompiled Contracts Check"
    
    # Test basic precompile accessibility via eth_call
    local precompiles=(
        "0x0000000000000000000000000000000000000100:P256"
        "0x0000000000000000000000000000000000000400:Bech32"
        "0x0000000000000000000000000000000000000800:Staking"
        "0x0000000000000000000000000000000000000801:Distribution"
        "0x0000000000000000000000000000000000000802:ICS20"
        "0x0000000000000000000000000000000000000804:Bank"
        "0x0000000000000000000000000000000000000805:Governance"
        "0x0000000000000000000000000000000000000806:Slashing"
        "0x0000000000000000000000000000000000000807:Evidence"
    )
    
    for precompile in "${precompiles[@]}"; do
        IFS=':' read -r address name <<< "$precompile"
        print_test "Precompile $name ($address)"
        
        # Try to call the precompile (this will likely fail but we check if it's recognized)
        local call_data='{"to":"'$address'","data":"0x"}'
        local response=$(make_jsonrpc_request "eth_call" "[$call_data, \"latest\"]")
        
        if [ -n "$response" ]; then
            local error=$(echo "$response" | jq -r '.error.message // "null"')
            if [ "$error" = "null" ] || [[ "$error" == *"execution reverted"* ]] || [[ "$error" == *"invalid opcode"* ]]; then
                print_success "$name precompile is recognized"
            else
                print_warning "$name precompile may not be active: $error"
            fi
        else
            print_failure "Cannot test $name precompile"
        fi
    done
}

# 5. Performance and Monitoring Check
check_monitoring() {
    print_header "5. Performance and Monitoring Check"
    
    # Prometheus metrics
    print_test "Prometheus metrics port ($METRICS_PORT)"
    if check_port $METRICS_PORT; then
        print_success "Prometheus metrics port is accessible"
        
        local metrics=$(make_smart_request ":$METRICS_PORT/metrics")
        if [[ "$metrics" == *"tendermint"* ]]; then
            print_success "Tendermint metrics are available"
        else
            print_warning "Tendermint metrics not found"
        fi
    else
        print_failure "Prometheus metrics port is not accessible"
    fi
    
    # EVM metrics
    print_test "EVM metrics port ($EVM_METRICS_PORT)"
    if check_port $EVM_METRICS_PORT; then
        print_success "EVM metrics port is accessible"
        
        local evm_metrics=$(make_smart_request ":$EVM_METRICS_PORT/debug/metrics/prometheus")
        if [ -n "$evm_metrics" ]; then
            print_success "EVM metrics are available"
        else
            print_warning "EVM metrics not found"
        fi
    else
        print_warning "EVM metrics port is not accessible (may be disabled)"
    fi
    
    # Check log files
    print_test "Log files"
    if [ -d "$GURUD_HOME/logs" ]; then
        print_success "Log directory exists"
        
        # Check for recent errors
        if [ -f "$GURUD_HOME/logs/gurud.log" ]; then
            local recent_errors=$(tail -n 100 "$GURUD_HOME/logs/gurud.log" | grep -i error | wc -l)
            if [ "$recent_errors" -eq 0 ]; then
                print_success "No recent errors in logs"
            else
                print_warning "Found $recent_errors recent errors in logs"
            fi
        fi
    else
        print_warning "Log directory not found (logs may be sent to stdout)"
    fi
}

# 6. Governance Check
check_governance() {
    print_header "6. Governance Check"
    
    print_test "Governance parameters"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local gov_params
        gov_params=$(gurud query gov params --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$gov_params" ]; then
            local voting_period=$(echo "$gov_params" | jq -r '.voting_params.voting_period // .params.voting_period // "null"')
            local min_deposit=$(echo "$gov_params" | jq -r '.deposit_params.min_deposit[0].amount // .params.min_deposit[0].amount // "null"')
            print_success "Governance accessible - Voting period: $voting_period, Min deposit: $min_deposit"
        else
            print_failure "Cannot query governance parameters"
        fi
        
        # Check active proposals
        print_test "Active proposals"
        local proposals
        proposals=$(gurud query gov proposals --status voting_period --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$proposals" ]; then
            local proposal_count=$(echo "$proposals" | jq '.proposals | length')
            print_success "Active proposals: $proposal_count"
        else
            print_warning "Cannot query proposals"
        fi
    else
        # Use REST API for remote hosts
        local gov_params=$(make_smart_request ":$API_PORT/cosmos/gov/v1beta1/params/voting")
        if [ -n "$gov_params" ]; then
            local voting_period=$(echo "$gov_params" | jq -r '.voting_params.voting_period // "null"')
            print_success "Governance accessible via REST API - Voting period: $voting_period"
            
            # Check active proposals
            print_test "Active proposals"
            local proposals=$(make_smart_request ":$API_PORT/cosmos/gov/v1beta1/proposals?proposal_status=2")
            if [ -n "$proposals" ]; then
                local proposal_count=$(echo "$proposals" | jq '.proposals | length')
                print_success "Active proposals: $proposal_count"
            else
                print_warning "Cannot query proposals via REST API"
            fi
        else
            print_failure "Cannot query governance parameters via REST API"
        fi
    fi
}

# 7. Security Check
check_security() {
    print_header "7. Security Check"
    
    # File permissions
    print_test "Configuration file permissions"
    if [ -f "$GURUD_HOME/config/priv_validator_key.json" ]; then
        local perms=$(stat -f "%Lp" "$GURUD_HOME/config/priv_validator_key.json" 2>/dev/null || stat -c "%a" "$GURUD_HOME/config/priv_validator_key.json" 2>/dev/null)
        if [ "$perms" = "600" ] || [ "$perms" = "400" ]; then
            print_success "Validator key permissions are secure ($perms)"
        else
            print_failure "Validator key permissions are too open ($perms)"
        fi
    else
        print_warning "Validator key file not found"
    fi
    
    # Check if dangerous APIs are disabled in production
    print_test "Dangerous API endpoints"
    local personal_response=$(make_jsonrpc_request "personal_listAccounts")
    if [ -n "$personal_response" ]; then
        local error=$(echo "$personal_response" | jq -r '.error.message // "null"')
        if [[ "$error" == *"method not found"* ]] || [[ "$error" == *"not supported"* ]]; then
            print_success "Personal API is disabled"
        else
            print_warning "Personal API may be enabled"
        fi
    fi
}

# 8. External Integrations Check
check_external_integrations() {
    print_header "8. External Integrations Check"
    
    # IBC channels
    print_test "IBC channels"
    if [ "$NETWORK_HOST" = "localhost" ]; then
        # Use local gurud command for localhost
        local ibc_channels
        ibc_channels=$(gurud query ibc channel channels --output json 2>/dev/null)
        if [ $? -eq 0 ] && [ -n "$ibc_channels" ]; then
            local channel_count=$(echo "$ibc_channels" | jq '.channels | length')
            print_success "IBC channels: $channel_count"
        else
            print_warning "Cannot query IBC channels"
        fi
    else
        # Use REST API for remote hosts
        local ibc_channels=$(make_smart_request ":$API_PORT/ibc/core/channel/v1/channels")
        if [ -n "$ibc_channels" ]; then
            local channel_count=$(echo "$ibc_channels" | jq '.channels | length')
            print_success "IBC channels: $channel_count"
        else
            print_warning "Cannot query IBC channels via REST API"
        fi
    fi
    
    # Oracle daemon
    print_test "Oracle daemon process"
    if pgrep -f "oracled" > /dev/null; then
        print_success "Oracle daemon is running"
    else
        print_warning "Oracle daemon not found"
    fi
}

# 9. API Endpoints Check
check_api_endpoints() {
    print_header "9. API Endpoints Check"
    
    # Cosmos REST API
    print_test "Cosmos REST API port ($API_PORT)"
    if check_port $API_PORT; then
        print_success "Cosmos REST API port is accessible"
        
        local node_info=$(make_smart_request ":$API_PORT/cosmos/base/tendermint/v1beta1/node_info")
        if [ -n "$node_info" ]; then
            local network=$(echo "$node_info" | jq -r '.default_node_info.network // "null"')
            if [ "$network" = "$CHAIN_ID" ]; then
                print_success "REST API network: $network"
            else
                print_warning "REST API network mismatch: $network"
            fi
        fi
    else
        print_warning "Cosmos REST API port is not accessible"
    fi
    
    # gRPC
    print_test "gRPC port ($GRPC_PORT)"
    if check_port $GRPC_PORT; then
        print_success "gRPC port is accessible"
    else
        print_warning "gRPC port is not accessible"
    fi
}

# Summary report
print_summary() {
    print_header "Test Summary"
    
    echo -e "Total tests run: $TOTAL_TESTS"
    echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
    echo -e "${RED}Failed: $FAILED_TESTS${NC}"
    
    local success_rate=$((PASSED_TESTS * 100 / TOTAL_TESTS))
    echo -e "Success rate: $success_rate%"
    
    if [ $FAILED_TESTS -eq 0 ]; then
        echo -e "\n${GREEN}🎉 All tests passed! Network appears to be healthy.${NC}"
        return 0
    elif [ $success_rate -ge 80 ]; then
        echo -e "\n${YELLOW}⚠️  Most tests passed, but some issues detected.${NC}"
        return 1
    else
        echo -e "\n${RED}❌ Multiple issues detected. Please review the failures.${NC}"
        return 2
    fi
}

# Main execution
main() {
    echo -e "${BLUE}"
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║              Guru Chain Network Health Check              ║"
    echo "║                     자동 체크 스크립트                    ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    
    echo "Network Host: $NETWORK_HOST"
    echo "Chain ID: $CHAIN_ID"
    echo "Home Directory: $GURUD_HOME"
    echo "Timestamp: $(date)"
    echo ""
    
    # Run all checks
    check_prerequisites
    check_network_health
    check_evm_compatibility
    check_custom_modules
    check_precompiled_contracts
    check_monitoring
    check_governance
    check_security
    check_external_integrations
    check_api_endpoints
    
    # Print summary and exit with appropriate code
    print_summary
    exit $?
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --host)
            NETWORK_HOST="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [options] [host]"
            echo ""
            echo "Arguments:"
            echo "  host                Network host to check (default: localhost)"
            echo ""
            echo "Options:"
            echo "  --host HOST         Network host to check"
            echo "  --help, -h          Show this help message"
            echo "  --config-check      Run only configuration checks"
            echo "  --network-check     Run only network health checks"
            echo "  --evm-check         Run only EVM compatibility checks"
            echo ""
            echo "Environment Variables:"
            echo "  GURUD_HOME         Home directory for gurud (default: ~/.gurud)"
            echo "  CHAIN_ID           Chain ID to verify (default: guru_631-1)"
            echo "  NETWORK_HOST       Network host (default: localhost)"
            echo "  RPC_PORT           CometBFT RPC port (default: 26657)"
            echo "  JSON_RPC_PORT      EVM JSON-RPC port (default: 8545)"
            echo ""
            echo "Examples:"
            echo "  $0                                    # Check localhost"
            echo "  $0 --host trpc.gurufin.io             # Check remote host"
            echo "  $0 trpc.gurufin.io                    # Check remote host (positional)"
            echo "  NETWORK_HOST=trpc.gurufin.io $0       # Check using env var"
            exit 0
            ;;
        --config-check)
            CHECK_MODE="config"
            shift
            ;;
        --network-check)
            CHECK_MODE="network"
            shift
            ;;
        --evm-check)
            CHECK_MODE="evm"
            shift
            ;;
        -*)
            echo "Unknown option: $1"
            exit 1
            ;;
        *)
            # Positional argument - treat as host
            NETWORK_HOST="$1"
            shift
            ;;
    esac
done

# Execute based on check mode
case "${CHECK_MODE:-full}" in
    config)
        check_prerequisites
        check_security
        ;;
    network)
        check_prerequisites
        check_network_health
        check_monitoring
        ;;
    evm)
        check_prerequisites
        check_evm_compatibility
        check_precompiled_contracts
        ;;
    *)
        main
        ;;
esac
