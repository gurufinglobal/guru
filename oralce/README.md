# GURU Oracle Daemon

The GURU Oracle Daemon is a distributed oracle system designed to securely and reliably collect external data and submit it to the GURU blockchain network. It provides real-time event processing and high-performance data collection through a TaskGroup-based concurrent execution architecture.

## System Architecture

### Core Components

The Oracle Daemon consists of four main components that work together to provide a complete oracle service:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Oracle Daemon                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│      ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐      │
│      │     Monitor     │    │   Scheduler     │    │   Submitter     │      │
│      │                 │    │                 │    │                 │      │
│      │ • Event Sub     │───▶│ • Job Store     │───▶│ • Tx Building   │      │
│      │ • Real-time     │    │ • TaskGroup     │    │ • Signing       │      │
│      │   Monitoring    │    │ • Periodic Exec │    │ • Broadcasting  │      │
│      │ • Account Filter│    │ • Result Queue  │    │ • Sequence Mgmt │      │
│      └─────────────────┘    └─────────────────┘    └─────────────────┘      │
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │                       Job Executor                                  │   │
│   │                                                                     │   │
│   │ • HTTP Client         • JSON Parsing       • Data Extraction        │   │
│   │ • Retry Mechanism     • Path Navigation    • Error Handling         │   │
│   │ • TaskGroup Exec      • Array Support      • Type Conversion        │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         GURU Blockchain Network                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│     ┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐       │
│     │  Oracle Module  │    │  Event System   │    │  State Machine  │       │
│     │                 │    │                 │    │                 │       │
│     │ • Request Mgmt  │    │ • Event Emit    │    │ • State Update  │       │
│     │ • Data Verify   │    │ • Subscription  │    │ • Consensus     │       │
│     │ • Result Store  │    │ • Notification  │    │ • Finality      │       │
│     └─────────────────┘    └─────────────────┘    └─────────────────┘       │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Data Flow Pipeline

```
External API
     │
     ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  HTTP Request   │───▶│  JSON Parsing   │───▶│ Data Extraction │
│                 │    │                 │    │                 │
│ • GET Request   │    │ • Object Parse  │    │ • Path-based    │
│ • Headers       │    │ • Array Handle  │    │ • Dot Notation  │
│ • Retry Logic   │    │ • Error Check   │    │ • Type Convert  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                                       │
                                                       ▼
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│ Blockchain Sub  │◀───│ Transaction     │◀───│  Result Queue   │
│                 │    │                 │    │                 │
│ • Broadcasting  │    │ • Msg Creation  │    │ • Job Results   │
│ • Response      │    │ • Signing       │    │ • Queue Mgmt    │
│ • Sequence Sync │    │ • Gas Calc      │    │ • Order Ensure  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## Event Processing System

### Event Types and Processing

The Oracle Daemon listens to three types of blockchain events:

#### 1. Register Event (Oracle Request Registration)
```go
// Subscription query
registerQuery := "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgRegisterOracleRequestDoc'"

// Processing flow:
1. Detect new oracle request registration on blockchain
2. Extract account list from request document
3. Verify if current daemon instance is assigned
4. Extract endpoint URL and parsing rules
5. Create Job object and store in job store
```

#### 2. Update Event (Oracle Request Update)
```go
// Subscription query
updateQuery := "tm.event='Tx' AND message.action='/guru.oracle.v1.MsgUpdateOracleRequestDoc'"

// Processing flow:
1. Detect oracle request updates
2. Extract changed configuration
3. Find corresponding job in active job store
4. Update job settings (URL, parsing rules, period)
```

#### 3. Complete Event (Data Collection Completion)
```go
// Subscription query
completeQuery := "tm.event='NewBlock' AND complete_oracle_data_set.request_id EXISTS"

// Processing flow:
1. Detect completion events in new blocks
2. Extract request ID and nonce information
3. Find corresponding job in active job store
4. Synchronize nonce and prepare for next collection cycle
```

## Configuration and Setup

### Home Directory Structure

The Oracle Daemon uses a default home directory structure that is automatically created on first run:

#### Default Home Directory
```bash
# Default location (auto-detected based on OS)
~/.oracled/                    # Base home directory

# Platform-specific locations:
# Linux/macOS: /home/username/.oracled
# Windows:     C:\Users\username\.oracled
```

#### Complete Directory Structure
```bash
~/.oracled/
├── config.toml               # Main configuration file (TOML format)
├── keyring-test/             # Test keyring storage (development)
│   ├── keyring-test.db       # SQLite database for test keys
│   └── *.info                # Key metadata files
├── keyring-file/             # File-based keyring storage (production)
│   ├── *.address             # Address files
│   └── *.info                # Encrypted key files
├── keyring-os/               # OS-native keyring storage (secure)
│   └── (OS-managed storage)  # Platform-specific secure storage
└── logs/                     # Log files directory
    ├── oracled.12345.log     # Current daemon log (PID-based naming)
    └── oracled.*.log         # Historical log files
```

#### Custom Home Directory
```bash
# Override default home directory
./oracled --home /custom/oracle/path
```

### Configuration File Structure

The daemon uses TOML format for configuration with the following structure:

#### Complete Configuration Schema
```toml
# ~/.oracled/config.toml
[chain]
id = 'guru_631-1'
endpoint = 'http://localhost:26657'

[key]
name = 'mykey'
keyring_dir = '/home/user/.oracled'
keyring_backend = 'test'

[gas]
limit = 70000
adjustment = 1.5
prices = '630000000000'

[retry]
max_attempts = 6
initial_backoff_sec = 1
max_backoff_sec = 8
circuit_breaker_failures = 5
circuit_breaker_window_sec = 30
circuit_breaker_cooldown_sec = 30

[http]
timeout_sec = 30
max_idle_conns = 1000
max_idle_per_host = 100
max_conns_per_host = 200
read_buffer_kb = 32
write_buffer_kb = 32
requests_per_sec = 20
idle_conn_timeout_sec = 90
disable_keep_alives = false
disable_compression = false
force_attempt_http2 = true
tls_handshake_timeout_sec = 10
expect_continue_timeout_sec = 1
```
