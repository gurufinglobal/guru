<!--
Guiding Principles:

Changelogs are for humans, not machines.
There should be an entry for every single version.
The same types of changes should be grouped.
Versions and sections should be linkable.
The latest version comes first.
The release date of each version is displayed.
Mention whether you follow Semantic Versioning.

Types of changes:

[Added] for new features.
[Changed] for changes in existing functionality.
[Deprecated] for soon-to-be removed features.
[Removed] for now removed features.
[Fixed] for any bug fixes.
[Security] in case of vulnerabilities.

Ref: https://keepachangelog.com/en/1.1.0/
-->

# Changelog

## [v1.0.8] - 2025-07-10

### Added
- **Oracle System**: Comprehensive Oracle module & Daemon implementation
  - Oracle daemon service for automated data processing and monitoring
  - Event handling and subscription management system with real-time updates
  - Job processing and worker management capabilities with scalable architecture
  - Transaction sequence number synchronization for reliable execution
  - Configuration management and enhanced error handling
  - Comprehensive logging and monitoring capabilities for observability
  - Integration with feemarket module for dynamic min_gas_price adjustment

- **EIP-6780 Implementation**: Complete support for EIP-6780 (SELFDESTRUCT changes)
  - Implementation of EIP-6780 core functionality for enhanced smart contract security
  - Integration with Ethereum v1.12.1 upgrade for latest EVM compatibility

## [v1.0.7] - 2025-03-18

### Added
- [[#30](https://github.com/GPTx-global/guru/pull/30)] Add EIP-5656 support
  - EIP-5656: MCOPY opcode for efficient memory copying
- [[#29](https://github.com/GPTx-global/guru/pull/29)] Add EIP-1153 support
  - EIP-1153: Transient storage opcodes (TSTORE/TLOAD)

### Changed
- [[#28](https://github.com/GPTx-global/guru/pull/28)] Fix test RPC functionality
- [[#27](https://github.com/GPTx-global/guru/pull/27)] Modify GitHub Actions test patterns

## [v1.0.6] - 2025-01-21

### Added
- [[#23](https://github.com/GPTx-global/guru/pull/23)] Add fee ratio functionality
  - Distribution module moderator and base address configuration
  - Enhanced fee distribution mechanisms with custom ratios

### Changed
- Update cosmos-sdk version to v0.46.13-ledger.3-guru.2
- Improve distribution module with custom fee allocation logic
- Add IBC-Go v6.1.1-guru.1 integration for enhanced cross-chain functionality
- Update test suites to support new distribution functionality

## [v1.0.5] - 2025-01-21

### Added
- [[#22](https://github.com/GPTx-global/guru/pull/22)] GitHub Actions CI/CD pipeline
  - Automated build, test, and release workflows
  - Dependency vulnerability checking and security scanning
  - Go 1.22 support and tooling improvements
  - Comprehensive test automation and coverage reporting
- [[#21](https://github.com/GPTx-global/guru/pull/21)] Documentation improvements
  - Updated CHANGELOG format following keepachangelog.com standards
  - Enhanced CONTRIBUTING guidelines and development workflow
  - Improved repository documentation and structure

## [v1.0.4] - 2025-01-02

### Changed
- [[#13](https://github.com/GPTx-global/guru/pull/13)] Change gas config and params.


## [v1.0.3] - 2025-01-02

### Changed
- Change module name.
- [[#10](https://github.com/GPTx-global/guru/pull/10)] Change docker image name.

### Removed
- [[#9](https://github.com/GPTx-global/guru/pull/9)] Remove unnecessary files.


## [v1.0.2] - 2024-12-24

### Removed
- Remove unnecessary Modules and Upgrades.


## [v1.0.1] - 2024-12-23

### Changed
- Change daemon, denom, cmd and etc.
- Change app name.


## [v1.0.0] - 2024-12-23

This is guru's basic version.

## Evmos's changelog
The changelog for evmos can be found [here](https://github.com/evmos/evmos/blob/main/CHANGELOG.md).
