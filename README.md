This is forked from cosmos/evm [v0.3.1](https://github.com/cosmos/evm/tree/v0.3.1).

# Guru Chain

[![version](https://img.shields.io/github/v/tag/gurufinglobal/guru.svg)](https://github.com/gurufinglobal/guru/releases/latest)
[![Go version](https://img.shields.io/badge/go-1.24.6+-green.svg)](https://github.com/moovweb/gvm)
<!-- admin widget setting: https://shields.io/badges/discord

[![Discord chat](https://img.shields.io/discord/1109002731580051466
.svg)](https://discord.gg/FJBTMgHEJg)
-->


Guru Chain is forked from Cosmos EVM [v0.3.1](https://github.com/cosmos/evm/releases/tag/v0.3.1) on 2025-08.

Audited by Sherlock (October 2025).
A link to the final audit report can be found [here](https://github.com/gurufinglobal/guru/blob/d03f48445d6ee3810f5becde9a334f2ae61cb059/docs/audits/Gurufin_Sherlock_Audit_final_Report_2025_10_31.pdf).

## Releases

Please do not depend on `main` as your production branch. Use [releases](https://github.com/gurufinglobal/guru/releases) instead.

## Minimum requirements

| Requirement | Notes              |
| ----------- |--------------------|
| Go version  | Go1.24.11 or higher |


# Quick Start
## git clone
```
git clone https://github.com/gurufinglobal/guru.git
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
- [Original Gurufin Whitepaper](https://img.gurufin.app/gurufin/white_paper.pdf)