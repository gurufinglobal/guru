This is forked from cosmos/evm [v0.3.1](https://github.com/cosmos/evm/tree/v0.3.1).

# Guru Chain

[![version](https://img.shields.io/github/v/tag/GPTx-global/guru-v2.svg)](https://github.com/GPTx-global/guru-v2/v2/releases/latest)
[![Go version](https://img.shields.io/badge/go-1.24.6+-green.svg)](https://github.com/moovweb/gvm)
<!-- admin widget setting: https://shields.io/badges/discord

[![Discord chat](https://img.shields.io/discord/1109002731580051466
.svg)](https://discord.gg/FJBTMgHEJg)
-->


[Guru Chain](https://github.com/GPTx-global/guru-v2/v2/PAPER.md) is forked from Cosmos EVM [v0.3.1](https://github.com/cosmos/evm/releases/tag/v0.3.1) on 2025-08.

## Releases

Please do not depend on `main` as your production branch. Use [releases](https://github.com/GPTx-global/guru-v2/v2/releases) instead.

## Minimum requirements

| Requirement | Notes              |
| ----------- |--------------------|
| Go version  | Go1.24.6 or higher |


# Quick Start
## git clone
```
git clone https://github.com/GPTx-global/guru-v2/v2.git
```

## Test & Cover
```
make test
make test-unit-cover
```

## Build
```
make build
make install
```
## Check version
```
gurud version
```

## Local Standalone
```
./local_node.sh
```
### Check Process
```
ps -ef | grep gurud | grep -v grep
```

## Tools

Benchmarking is provided by [`tm-load-test`](https://github.com/informalsystems/tm-load-test).

For more detailed information, please refer to the [Guru documentation](https://docs.gurufin.com).

## Applications

- [Cosmos SDK](http://github.com/reapchain/cosmos-sdk); a cryptocurrency application framework

## Research

- [The latest gossip on BFT consensus](https://arxiv.org/abs/1807.04938)
- Original Guru Whitepaper