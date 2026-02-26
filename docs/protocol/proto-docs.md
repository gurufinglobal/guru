# Protocol Documentation
#
# This template is used by protoc-gen-doc (buf "doc" plugin).
# Keep it simple and stable so proto generation doesn't fail in dev envs.
## cosmos/evm/crypto/v1/ethsecp256k1/keys.proto
### Messages
- **PrivKey**
- **PubKey**


## cosmos/evm/erc20/v1/erc20.proto
### Messages
- **Allowance**
- **ProposalMetadata**
- **RegisterCoinProposal**
- **RegisterERC20Proposal**
- **ToggleTokenConversionProposal**
- **TokenPair**
### Enums
- **Owner**


## cosmos/evm/erc20/v1/events.proto
### Messages
- **EventConvertCoin**
- **EventConvertERC20**
- **EventRegisterPair**
- **EventToggleTokenConversion**


## cosmos/evm/erc20/v1/genesis.proto
### Messages
- **GenesisState**
- **Params**


## cosmos/evm/erc20/v1/query.proto
### Services
- **Query**
### Messages
- **QueryParamsRequest**
- **QueryParamsResponse**
- **QueryTokenPairRequest**
- **QueryTokenPairResponse**
- **QueryTokenPairsRequest**
- **QueryTokenPairsResponse**


## cosmos/evm/erc20/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgConvertCoin**
- **MsgConvertCoinResponse**
- **MsgConvertERC20**
- **MsgConvertERC20Response**
- **MsgRegisterERC20**
- **MsgRegisterERC20Response**
- **MsgToggleConversion**
- **MsgToggleConversionResponse**
- **MsgUpdateParams**
- **MsgUpdateParamsResponse**


## cosmos/evm/feemarket/v1/events.proto
### Messages
- **EventBlockGas**
- **EventFeeMarket**


## cosmos/evm/feemarket/v1/feemarket.proto
### Messages
- **Params**


## cosmos/evm/feemarket/v1/genesis.proto
### Messages
- **GenesisState**


## cosmos/evm/feemarket/v1/query.proto
### Services
- **Query**
### Messages
- **QueryBaseFeeRequest**
- **QueryBaseFeeResponse**
- **QueryBlockGasRequest**
- **QueryBlockGasResponse**
- **QueryParamsRequest**
- **QueryParamsResponse**


## cosmos/evm/feemarket/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgUpdateParams**
- **MsgUpdateParamsResponse**


## cosmos/evm/precisebank/v1/genesis.proto
### Messages
- **FractionalBalance**
- **GenesisState**


## cosmos/evm/precisebank/v1/query.proto
### Services
- **Query**
### Messages
- **QueryFractionalBalanceRequest**
- **QueryFractionalBalanceResponse**
- **QueryRemainderRequest**
- **QueryRemainderResponse**


## cosmos/evm/types/v1/dynamic_fee.proto
### Messages
- **ExtensionOptionDynamicFeeTx**


## cosmos/evm/types/v1/indexer.proto
### Messages
- **TxResult**


## cosmos/evm/types/v1/web3.proto
### Messages
- **ExtensionOptionsWeb3Tx**


## cosmos/evm/vm/v1/events.proto
### Messages
- **EventBlockBloom**
- **EventEthereumTx**
- **EventMessage**
- **EventTxLog**


## cosmos/evm/vm/v1/evm.proto
### Messages
- **AccessControl**
- **AccessControlType**
- **AccessTuple**
- **ChainConfig**
- **Log**
- **Params**
- **State**
- **TraceConfig**
- **TransactionLogs**
- **TxResult**
### Enums
- **AccessType**


## cosmos/evm/vm/v1/genesis.proto
### Messages
- **GenesisAccount**
- **GenesisState**


## cosmos/evm/vm/v1/tx.proto
### Services
- **Msg**
### Messages
- **AccessListTx**
- **DynamicFeeTx**
- **ExtensionOptionsEthereumTx**
- **LegacyTx**
- **MsgEthereumTx**
- **MsgEthereumTxResponse**
- **MsgUpdateParams**
- **MsgUpdateParamsResponse**


## cosmos/evm/vm/v1/query.proto
### Services
- **Query**
### Messages
- **EstimateGasResponse**
- **EthCallRequest**
- **QueryAccountRequest**
- **QueryAccountResponse**
- **QueryBalanceRequest**
- **QueryBalanceResponse**
- **QueryBaseFeeRequest**
- **QueryBaseFeeResponse**
- **QueryCodeRequest**
- **QueryCodeResponse**
- **QueryConfigRequest**
- **QueryConfigResponse**
- **QueryCosmosAccountRequest**
- **QueryCosmosAccountResponse**
- **QueryGlobalMinGasPriceRequest**
- **QueryGlobalMinGasPriceResponse**
- **QueryParamsRequest**
- **QueryParamsResponse**
- **QueryStorageRequest**
- **QueryStorageResponse**
- **QueryTraceBlockRequest**
- **QueryTraceBlockResponse**
- **QueryTraceTxRequest**
- **QueryTraceTxResponse**
- **QueryTxLogsRequest**
- **QueryTxLogsResponse**
- **QueryValidatorAccountRequest**
- **QueryValidatorAccountResponse**


## guru/bex/v1/bex.proto
### Messages
- **Exchange**
- **MetadataEntry**
- **RateRegistry**
- **Ratemeter**


## guru/bex/v1/genesis.proto
### Messages
- **GenesisState**


## guru/bex/v1/query.proto
### Services
- **Query**
### Messages
- **QueryAvailableFeesRequest**
- **QueryAvailableFeesResponse**
- **QueryCollectedFeesRequest**
- **QueryCollectedFeesResponse**
- **QueryExchangesRequest**
- **QueryExchangesResponse**
- **QueryIsAdminRequest**
- **QueryIsAdminResponse**
- **QueryLockedFeesRequest**
- **QueryLockedFeesResponse**
- **QueryModeratorAddressRequest**
- **QueryModeratorAddressResponse**
- **QueryNextExchangeIdRequest**
- **QueryNextExchangeIdResponse**
- **QueryRatemeterRequest**
- **QueryRatemeterResponse**


## guru/bex/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgChangeBexModerator**
- **MsgChangeBexModeratorResponse**
- **MsgRegisterAdmin**
- **MsgRegisterAdminResponse**
- **MsgRegisterExchange**
- **MsgRegisterExchangeResponse**
- **MsgRemoveAdmin**
- **MsgRemoveAdminResponse**
- **MsgUpdateExchange**
- **MsgUpdateExchangeResponse**
- **MsgUpdateRatemeter**
- **MsgUpdateRatemeterResponse**
- **MsgWithdrawFees**
- **MsgWithdrawFeesResponse**


## guru/feepolicy/v1/feepolicy.proto
### Messages
- **AccountDiscount**
- **AccountDiscounts**
- **Discount**
- **Moderator**
- **ModuleDiscount**


## guru/feepolicy/v1/genesis.proto
### Messages
- **GenesisState**


## guru/feepolicy/v1/query.proto
### Services
- **Query**
### Messages
- **QueryDiscountRequest**
- **QueryDiscountResponse**
- **QueryDiscountsRequest**
- **QueryDiscountsResponse**
- **QueryModeratorAddressRequest**
- **QueryModeratorAddressResponse**


## guru/feepolicy/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgChangeModerator**
- **MsgChangeModeratorResponse**
- **MsgRegisterDiscounts**
- **MsgRegisterDiscountsResponse**
- **MsgRemoveDiscounts**
- **MsgRemoveDiscountsResponse**


## guru/feeproxy/v1/feeproxy.proto
### Messages
- **Params**


## guru/feeproxy/v1/genesis.proto
### Messages
- **GenesisState**


## guru/feeproxy/v1/query.proto
### Services
- **Query**
### Messages
- **QueryAdminAddressRequest**
- **QueryAdminAddressResponse**
- **QueryFeePercentageRequest**
- **QueryFeePercentageResponse**
- **QueryIsAdminRequest**
- **QueryIsAdminResponse**
- **QueryModeratorAddressRequest**
- **QueryModeratorAddressResponse**
- **QueryParamsRequest**
- **QueryParamsResponse**
- **QueryReserveAddressRequest**
- **QueryReserveAddressResponse**


## guru/feeproxy/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgRegisterAdmin**
- **MsgRegisterAdminResponse**
- **MsgUpdateFeePercentage**
- **MsgUpdateFeePercentageResponse**
- **MsgUpdateReserveAddress**
- **MsgUpdateReserveAddressResponse**


## guru/oracle/v1/oracle.proto
### Messages
- **DataSet**
- **OracleEndpoint**
- **OracleRequestDoc**
- **SubmitDataSet**
### Enums
- **AggregationRule**
- **OracleType**
- **RequestStatus**


## guru/oracle/v1/genesis.proto
### Messages
- **GenesisState**
- **Params**


## guru/oracle/v1/query.proto
### Services
- **Query**
### Messages
- **QueryModeratorAddressRequest**
- **QueryModeratorAddressResponse**
- **QueryOracleDataRequest**
- **QueryOracleDataResponse**
- **QueryOracleRequestDocRequest**
- **QueryOracleRequestDocResponse**
- **QueryOracleRequestDocsRequest**
- **QueryOracleRequestDocsResponse**
- **QueryOracleSubmitDataRequest**
- **QueryOracleSubmitDataResponse**
- **QueryParamsRequest**
- **QueryParamsResponse**


## guru/oracle/v1/tx.proto
### Services
- **Msg**
### Messages
- **MsgRegisterOracleRequestDoc**
- **MsgRegisterOracleRequestDocResponse**
- **MsgSubmitOracleData**
- **MsgSubmitOracleDataResponse**
- **MsgUpdateModeratorAddress**
- **MsgUpdateModeratorAddressResponse**
- **MsgUpdateOracleRequestDoc**
- **MsgUpdateOracleRequestDocResponse**
- **MsgUpdateParams**
- **MsgUpdateParamsResponse**


## guru/transwap/v1/denomtrace.proto
### Messages
- **DenomTrace**


## guru/transwap/v1/token.proto
### Messages
- **Denom**
- **Hop**
- **Token**


## guru/transwap/v1/genesis.proto
### Messages
- **GenesisState**


## guru/transwap/v1/packet.proto
### Messages
- **FungibleTokenPacketData**
- **TransferPacketData**


## guru/transwap/v1/query.proto
### Services
- **Query**
### Messages
- **QueryDenomHashRequest**
- **QueryDenomHashResponse**
- **QueryDenomRequest**
- **QueryDenomResponse**
- **QueryDenomsRequest**
- **QueryDenomsResponse**
- **QueryEscrowAddressRequest**
- **QueryEscrowAddressResponse**
- **QueryTotalEscrowForDenomRequest**
- **QueryTotalEscrowForDenomResponse**



