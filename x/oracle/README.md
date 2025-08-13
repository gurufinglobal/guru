# Oracle Module

## Overview

The oracle module is responsible for managing oracle data in the blockchain. It provides functionality for registering, updating, and managing oracle request documents, as well as submitting and aggregating oracle data.

## Features

- Oracle Request Document Management
  - Register new oracle request documents
  - Update existing oracle request documents
  - Query oracle request documents

- Oracle Data Management
  - Submit oracle data
  - Aggregate oracle data based on different rules (AVG, MIN, MAX, MEDIAN)
  - Query oracle data

- Moderator Management
  - Update moderator address
  - Validate moderator permissions

## Messages

### Register Oracle Request Document
```go
MsgRegisterOracleRequestDoc
- ModeratorAddress: string
- RequestDoc: OracleRequestDoc
```

### Update Oracle Request Document
```go
MsgUpdateOracleRequestDoc
- ModeratorAddress: string
- RequestDoc: OracleRequestDoc
- Reason: string
```

### Submit Oracle Data
```go
MsgSubmitOracleData
- AuthorityAddress: string
- DataSet: SubmitDataSet
```

### Update Moderator Address
```go
MsgUpdateModeratorAddress
- ModeratorAddress: string
- NewModeratorAddress: string
```

## Queries

### Oracle Request Document
```go
QueryOracleRequestDocRequest
- RequestId: uint64
```

### Oracle Data
```go
QueryOracleDataRequest
- RequestId: uint64
- Nonce: uint64
```

## Events

### Register Oracle Request Document
```go
EventTypeRegisterOracleRequestDoc
- AttributeKeyRequestId
- AttributeKeyOracleType
- AttributeKeyName
- AttributeKeyDescription
- AttributeKeyPeriod
- AttributeKeyAccountList
- AttributeKeyEndpoints
- AttributeKeyAggregationRule
- AttributeKeyStatus
- AttributeKeyCreator
```

### Update Oracle Request Document
```go
EventTypeUpdateOracleRequestDoc
- AttributeKeyRequestId
- AttributeKeyOracleType
- AttributeKeyName
- AttributeKeyDescription
- AttributeKeyPeriod
- AttributeKeyAccountList
- AttributeKeyEndpoints
- AttributeKeyAggregationRule
- AttributeKeyStatus
- AttributeKeyNonce
- AttributeKeyCreator
```

### Submit Oracle Data
```go
EventTypeSubmitOracleData
- AttributeKeyRequestId
- AttributeKeyNonce
- AttributeKeyRawData
- AttributeKeyFromAddress
```

### Update Moderator Address
```go
EventTypeUpdateModeratorAddress
- AttributeKeyModeratorAddress
```

## Aggregation Rules

The module supports the following aggregation rules:

- AGGREGATION_RULE_AVG: Calculate the average of all submitted values
- AGGREGATION_RULE_MIN: Use the minimum value from all submissions
- AGGREGATION_RULE_MAX: Use the maximum value from all submissions
- AGGREGATION_RULE_MEDIAN: Calculate the median of all submitted values

## Authorization

- Only the moderator can register and update oracle request documents
- Only authorized accounts can submit oracle data
- Only the current moderator can update the moderator address

## State

The module maintains the following state:

- Oracle Request Documents
- Oracle Data Sets
- Moderator Address
- Oracle Request Document Count

## Hooks

The module provides hooks for external modules to react to oracle events:

- AfterOracleEnd: Called after an oracle data set is completed

## Genesis State

The Oracle module's genesis state contains the following parameters:

```json
{
  "oracle": {
    "params": {
      "enable_oracle": true,
      "submit_window": 3600,
      "min_submit_per_window": "0.5",
      "slash_fraction_downtime": "0.01"
    },
    "oracle_request_doc_count": 0,
    "oracle_request_docs": [],
    "moderator_address": "guru1..."
  }
}
```

### Parameters

- `enable_oracle`: Whether the oracle module is enabled
- `submit_window`: The window within which oracle data is expected to be submitted
- `min_submit_per_window`: Minimum number of submissions required per window (as a decimal)
- `slash_fraction_downtime`: Fraction of stake to slash for downtime (as a decimal)

### Export Genesis State

To export the current state of the Oracle module:

```bash
gurud export --home <path-to-home> | jq '.app_state.oracle'
```

### Import Genesis State

To import a genesis state for the Oracle module:

```bash
# Create a new genesis file with oracle state
jq '.app_state.oracle = {
  "params": {
    "enable_oracle": true,
    "submit_window": 3600,
    "min_submit_per_window": "0.5",
    "slash_fraction_downtime": "0.01"
  },
  "oracle_request_doc_count": 0,
  "oracle_request_docs": [],
  "moderator_address": "guru1..."
}' genesis.json > new-genesis.json

# Replace the old genesis file
mv new-genesis.json genesis.json
```

## Transactions

### Register Oracle Request Document

Register a new oracle request document.

```bash
gurud tx oracle register-request [path/to/request-doc.json]
```

### Update Oracle Request Document

Update an existing oracle request document.

```bash
gurud tx oracle update-request [path/to/request-doc.json] [reason]
```

### Submit Oracle Data

Submit oracle data for a specific request.

```bash
gurud tx oracle submit-data [request-id] [nonce] [raw-data]
```

### Update Moderator Address

Update the moderator address for the oracle module.

```bash
gurud tx oracle update-moderator-address [moderator-address]
```

## Queries

### Parameters

Query the current oracle parameters.

```bash
gurud query oracle params
```

### Oracle Request Document

Query a specific oracle request document by ID.

```bash
gurud query oracle request-doc [request-id]
```

### Oracle Data

Query oracle data for a specific request.

```bash
gurud query oracle data [request-id]
```

### Oracle Submit Data

Query oracle submit data for a request. You can provide [request-id], [nonce], and [provider-account] as arguments. However, if you only provide [request-id] and [nonce], you can view the list of submitted data corresponding to the [nonce] of the specified [request-id].

```bash
gurud query oracle submit-data [request-id] [nonce] [provider-account]
```

### Oracle Request Documents

Query all oracle request documents.

```bash
gurud query oracle request-docs
```

### Moderator Address

Query the current moderator address.

```bash
gurud query oracle moderator-address
```

## CLI Examples

### Register a New Oracle Request

### Oracle Types

The Oracle module supports the following types of oracle data:

| Constant | Value | Description |
| --- | --- | --- |
| `ORACLE_TYPE_UNSPECIFIED` | 0 | Default value, should not be used |
| `ORACLE_TYPE_MIN_GAS_PRICE` | 1 | Minimum gas price oracle for network fee estimation |
| `ORACLE_TYPE_CURRENCY` | 2 | Exchange rate and foreign exchange data |
| `ORACLE_TYPE_STOCK` | 3 | Stock market data and indices |
| `ORACLE_TYPE_CRYPTO` | 4 | Cryptocurrency price and market data |

Each oracle request must specify the type of data it requires using one of these oracle types.

### Aggregation Rules

The Oracle module supports the following rules for aggregating oracle data:

| Constant | Value | Description |
| --- | --- | --- |
| `AGGREGATION_RULE_UNSPECIFIED` | 0 | Default value, should not be used |
| `AGGREGATION_RULE_AVG` | 1 | Use average value for data aggregation |
| `AGGREGATION_RULE_MIN` | 2 | Use minimum value for data aggregation |
| `AGGREGATION_RULE_MAX` | 3 | Use maximum value for data aggregation |
| `AGGREGATION_RULE_MEDIAN` | 4 | Use median value for data aggregation |

Each oracle request must specify the rule for aggregating data using one of these aggregation rules.

### Request Statuses

The Oracle module supports the following statuses for oracle requests:

| Constant | Value | Description |
| --- | --- | --- |
| `REQUEST_STATUS_UNSPECIFIED` | 0 | Default value, should not be used |
| `REQUEST_STATUS_ENABLED` | 1 | Request is enabled |
| `REQUEST_STATUS_PAUSED` | 2 | Request is paused |
| `REQUEST_STATUS_DISABLED` | 3 | Request is disabled |

Each oracle request must specify its current status using one of these request statuses.

```bash
# Create a request document JSON file
# request_id is omitted.
cat > request.json << EOF
{
  "oracle_type": 1,
  "name": "BTC/USD Price Oracle",
  "description": "Provides real-time BTC/USD price data from multiple sources",
  "period": 60,
  "account_list": [
    "guru1...",
    "guru1..."
  ],
  "quorum": 3,
  "endpoints": [
    {
		"url": "https://api.coinbase.com/v2/prices/BTC-USD/spot",
		"parse_rule": "data.amount"
	},
	{
		"url": "https://api.coinbase.com/v2/prices/ETH-USD/spot",
		"parse_rule": "data.amount"
	}
  ],
  "aggregation_rule": 1,
  "status": 1,
}
EOF

# Register the request
gurud tx oracle register-request request.json --from mykey
```

### Update an Oracle Request

```bash
# Create an updated request document JSON file
# It is mandatory to include the request_id. Only [period, status, account_list, quorum, endpoints, parser_rule, aggregation_rule] can be updated. Remove any items that do not need to be updated.
cat > updated_request.json << EOF
{
  "request_id": 1,
  "period": 30,
  "account_list": [
    "guru1...",
    "guru1...",
    "guru1..."
  ],
  "quorum": 4,
  "endpoints": [
    {
		"url": "https://api.coinbase.com/v2/prices/BTC-USD/spot",
		"parse_rule": "data.amount"
	},
	{
		"url": "https://api.coinbase.com/v2/prices/ETH-USD/spot",
		"parse_rule": "data.amount"
	}
  ],
  "aggregation_rule": 4,
  "status": 1,
}
EOF

# Update the request with a reason
gurud tx oracle update-request updated_request.json "Improving data reliability and update frequency" --from mykey
```

### Submit Oracle Data

```bash
# Submit data for request ID 1 with nonce 1
gurud tx oracle submit-data 1 1 100 --from mykey
```

### Query Oracle Data

```bash
# Query data for request ID 1
gurud query oracle data 1
```

### Update Moderator

```bash
# Update moderator address
gurud tx oracle update-moderator-address guru1... --from current-moderator-address
```
