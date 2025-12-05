# Guru Chain Scripts

Collection of utility scripts for Guru Chain development and operations.

## 📁 Script Overview

| Script | Purpose | Category |
|--------|---------|----------|
| `network_health_check.sh` | Automated network health monitoring | **Operations/Monitoring** |
| `generate_protos.sh` | Generate protobuf files (gogo) | **Development/Build** |
| `generate_protos_pulsar.sh` | Generate protobuf files (pulsar) | **Development/Build** |
| `run-solidity-tests.sh` | Execute Solidity tests | **Testing** |
| `compile_smart_contracts/` | Smart contract compilation | **Development/Build** |

---

## 🔧 Operations/Monitoring Scripts

### Network Health Check (`network_health_check.sh`)

**Purpose**: Comprehensive automated health monitoring for Guru Chain networks

#### Key Features
- ✅ **34 automated checks** across all network components
- ✅ **Local/Remote** network support
- ✅ **HTTPS/HTTP** automatic detection
- ✅ **Colored output** with detailed reporting

#### Usage
```bash
# Check local network
./check_network.sh

# Check remote network (multiple methods)
./check_network.sh trpc.gurufin.io
./check_network.sh --host trpc.gurufin.io
NETWORK_HOST=trpc.gurufin.io ./check_network.sh

# Partial checks
./check_network.sh --network-check  # Basic network only
./check_network.sh --evm-check      # EVM compatibility only
./check_network.sh --config-check   # Security settings only
```

#### Check Categories (9 sections)

**1. Basic Network Health**
- Node process, RPC ports, block production, P2P connections, validators

**2. EVM Compatibility**
- JSON-RPC, Chain ID (631), Web3 client, latest blocks

**3. Custom Modules**
- ERC20, Fee Market, Oracle, Precise Bank, Fee Policy

**4. Precompiled Contracts (9 contracts)**
- P256, Bech32, Staking, Distribution, ICS20, Bank, Governance, Slashing, Evidence

**5. Monitoring**
- Prometheus metrics, EVM metrics, log files

**6. Governance**
- Parameters, active proposals

**7. Security**
- File permissions, dangerous API checks

**8. External Integrations**
- IBC channels, Oracle daemon

**9. API Endpoints**
- Cosmos REST API, gRPC

#### Environment Variables
```bash
export NETWORK_HOST="trpc.gurufin.io"  # Network host
export CHAIN_ID="guru_631-1"           # Chain ID
export JSON_RPC_PORT="8545"            # EVM JSON-RPC port
export API_PORT="1317"                 # Cosmos REST API port
```

---

## 🛠️ Development/Build Scripts

### Protobuf Generation Scripts

#### `generate_protos.sh` (Gogo Protobuf)
**Purpose**: Generate protobuf files for legacy gogo/protobuf API

```bash
# Run in Docker environment (recommended)
docker run --network host --rm -v $(pwd):/workspace --workdir /workspace \
  ghcr.io/cosmos/proto-builder:v0.11.6 sh ./scripts/generate_protos.sh

# Or run directly
./scripts/generate_protos.sh
```

**Features**:
- Processes only proto files with guru-v2 go_package option
- Uses buf generate with gogo template
- Moves generated files to appropriate locations

#### `generate_protos_pulsar.sh` (Google Protobuf)
**Purpose**: Generate protobuf files for new google.golang.org/protobuf API

```bash
./scripts/generate_protos_pulsar.sh
```

**Features**:
- Cleans API directory before generation
- Uses pulsar template
- Supports latest protobuf API

### Smart Contract Compilation (`compile_smart_contracts/`)

**Purpose**: Compile all Solidity smart contracts in the repository using Hardhat

#### Usage
```bash
# Run compilation
make contracts-compile
# Or
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --compile

# Clean artifacts
make contracts-clean
# Or  
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --clean

# Add new contract
make contracts-add CONTRACT=path/to/contract.sol
# Or
python3 scripts/compile_smart_contracts/compile_smart_contracts.py --add path/to/contract.sol
```

**Features**:
- Automatically collects .sol files from entire repository
- Batch compilation using Hardhat project
- Updates only existing JSON files (new files require explicit addition)
- Configurable ignore patterns for files/folders

**Architecture**:
```
scripts/compile_smart_contracts/
├── compile_smart_contracts.py  # Main script
├── test_compile_smart_contracts.py  # Tests
├── README.md                   # Detailed usage
└── testdata/                   # Test data
    ├── hardhat.config.js
    ├── package.json
    └── solidity/
        └── SimpleContract.sol
```

---

## 🧪 Testing Scripts

### Solidity Tests (`run-solidity-tests.sh`)

**Purpose**: Execute Solidity smart contract integration tests

```bash
./scripts/run-solidity-tests.sh
```

**Process**:
1. Clean existing test data
2. Build gurud binary (`make install`)
3. Check and install Yarn if needed
4. Install dependencies (`yarn install`)
5. Run tests on Cosmos network (`yarn test --network cosmos`)

**Requirements**:
- Node.js & Yarn
- Go development environment
- Test files in `tests/solidity` directory

---

## 🚀 Quick Start Guide

### 1. Network Health Check
```bash
# Check local node
./check_network.sh

# Check mainnet  
./check_network.sh trpc.gurufin.io
```

### 2. Development Setup
```bash
# Generate protobuf files
./scripts/generate_protos.sh
./scripts/generate_protos_pulsar.sh

# Compile smart contracts
make contracts-compile
```

### 3. Run Tests
```bash
# Solidity tests
./scripts/run-solidity-tests.sh
```

---

## 📊 Output Examples

### Network Health Check Results
```
╔═══════════════════════════════════════════════════════════╗
║              Guru Chain Network Health Check              ║
║                   Automated Check Script                  ║
╚═══════════════════════════════════════════════════════════╝

Network Host: trpc.gurufin.io
Chain ID: guru_631-1
Timestamp: 2024-01-15 10:30:45

=== 1. Basic Network Health Check ===
Testing: Node process status
✓ PASS: gurud process is running
Testing: CometBFT RPC port (26657)
✓ PASS: CometBFT RPC port is accessible
Testing: Block production
✓ PASS: Block height: 78504
✓ PASS: Node is fully synced
Testing: P2P connections
✓ PASS: Connected to 4 peers

=== Test Summary ===
Total tests run: 34
Passed: 34
Failed: 0
Success rate: 100%

🎉 All tests passed! Network appears to be healthy.
```

---

## ⚙️ Advanced Configuration

### Automation Setup

#### Cron Jobs
```bash
# Check network every 10 minutes
*/10 * * * * /path/to/check_network.sh >> /var/log/guru_health.log 2>&1

# Detailed check daily at 9 AM
0 9 * * * /path/to/check_network.sh > /var/log/guru_daily_health.log 2>&1
```

#### CI/CD Pipeline
```bash
# Automated verification after deployment
./check_network.sh && echo "Deployment successful" || echo "Deployment failed"
```

### Docker Environment
```bash
# Generate protobuf in Docker
docker run --network host --rm -v $(pwd):/workspace --workdir /workspace \
  ghcr.io/cosmos/proto-builder:v0.11.6 sh ./scripts/generate_protos.sh
```

---

## 🔧 Troubleshooting

### Network Check Issues
```bash
# Port access denied
sudo ufw status
sudo netstat -tlnp | grep :8545

# Permission problems
chmod +x scripts/network_health_check.sh
chmod 600 ~/.gurud/config/priv_validator_key.json
```

### Build Issues
```bash
# Protobuf generation failure
buf --version
protoc --version

# Smart contract compilation failure
node --version
yarn --version
```

---

## 📋 Exit Codes

### Network Health Check
- `0`: All tests passed (100%)
- `1`: Some tests failed (80%+ success rate)
- `2`: Many tests failed (<80% success rate)

### Other Scripts
- `0`: Success
- `1`: Failure (check error messages)

---

## 📚 Additional Resources

- [Network Launch Checklist](../NETWORK_LAUNCH_CHECKLIST.md)
- [Guru Chain Documentation](https://docs.gurufin.com)
- [Project README](../README.md)