package app

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/gov"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/mint"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	"github.com/cosmos/cosmos-sdk/x/staking"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/evm/x/feemarket"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	gogogrpc "github.com/cosmos/gogoproto/grpc"
	googlegrpc "google.golang.org/grpc"

	"github.com/gurufinglobal/guru/v2/config"
)

const feeMarketMsgServiceName = "cosmos.evm.feemarket.v1.Msg"

var maximumFeeMarketGasPrice = sdkmath.LegacyNewDec(1_000_000_000_000_000_000)

type msgServerDecorator func(any) any

type trackedMsgServerDecorator struct {
	target     string
	decorate   msgServerDecorator
	matchCount int
}

// msgDecoratingConfigurator preserves every upstream query and migration
// registration while replacing only a selected transaction service.
type msgDecoratingConfigurator struct {
	module.Configurator
	guard *trackedMsgServerDecorator
}

func (cfg msgDecoratingConfigurator) MsgServer() gogogrpc.Server {
	return msgDecoratingServer{
		delegate: cfg.Configurator.MsgServer(),
		guard:    cfg.guard,
	}
}

type msgDecoratingServer struct {
	delegate gogogrpc.Server
	guard    *trackedMsgServerDecorator
}

func (server msgDecoratingServer) RegisterService(description *googlegrpc.ServiceDesc, implementation any) {
	if description.ServiceName == server.guard.target {
		server.guard.matchCount++
		implementation = server.guard.decorate(implementation)
	}
	server.delegate.RegisterService(description, implementation)
}

func registerServicesWithMsgServerGuard(
	cfg module.Configurator,
	target string,
	decorate msgServerDecorator,
	register func(module.Configurator),
) {
	guard := &trackedMsgServerDecorator{target: target, decorate: decorate}
	register(msgDecoratingConfigurator{Configurator: cfg, guard: guard})
	if guard.matchCount != 1 {
		panic(fmt.Errorf("expected exactly one %s registration, got %d", target, guard.matchCount))
	}
}

func invalidParameterPolicy(err error) error {
	return errorsmod.Wrap(sdkerrors.ErrInvalidRequest, err.Error())
}

func nilParameterUpdate(moduleName string) error {
	return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "%s parameter update request cannot be nil", moduleName)
}

func validateStakingParameterPolicy(params stakingtypes.Params) error {
	if params.BondDenom != config.BaseDenom {
		return fmt.Errorf("staking bond denom must be %q, got %q", config.BaseDenom, params.BondDenom)
	}
	return nil
}

type guardedStakingMsgServer struct {
	stakingtypes.MsgServer
}

func (server *guardedStakingMsgServer) UpdateParams(
	ctx context.Context,
	req *stakingtypes.MsgUpdateParams,
) (*stakingtypes.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, nilParameterUpdate(stakingtypes.ModuleName)
	}
	if err := req.Params.Validate(); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	if err := validateStakingParameterPolicy(req.Params); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	return server.MsgServer.UpdateParams(ctx, req)
}

type guardedStakingAppModule struct {
	staking.AppModule
}

func newGuardedStakingAppModule(upstream staking.AppModule) guardedStakingAppModule {
	return guardedStakingAppModule{AppModule: upstream}
}

func (appModule guardedStakingAppModule) RegisterServices(cfg module.Configurator) {
	registerServicesWithMsgServerGuard(
		cfg,
		stakingtypes.Msg_serviceDesc.ServiceName,
		func(implementation any) any {
			server, ok := implementation.(stakingtypes.MsgServer)
			if !ok {
				panic(fmt.Errorf("unexpected staking MsgServer implementation %T", implementation))
			}
			return &guardedStakingMsgServer{MsgServer: server}
		},
		appModule.AppModule.RegisterServices,
	)
}

func validateMintParameterPolicy(params minttypes.Params) error {
	if params.MintDenom != config.BaseDenom {
		return fmt.Errorf("mint denom must be %q, got %q", config.BaseDenom, params.MintDenom)
	}
	return nil
}

type guardedMintMsgServer struct {
	minttypes.MsgServer
}

func (server *guardedMintMsgServer) UpdateParams(
	ctx context.Context,
	req *minttypes.MsgUpdateParams,
) (*minttypes.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, nilParameterUpdate(minttypes.ModuleName)
	}
	if err := req.Params.Validate(); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	if err := validateMintParameterPolicy(req.Params); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	return server.MsgServer.UpdateParams(ctx, req)
}

type guardedMintAppModule struct {
	mint.AppModule
}

func newGuardedMintAppModule(upstream mint.AppModule) guardedMintAppModule {
	return guardedMintAppModule{AppModule: upstream}
}

func (appModule guardedMintAppModule) RegisterServices(cfg module.Configurator) {
	registerServicesWithMsgServerGuard(
		cfg,
		minttypes.Msg_serviceDesc.ServiceName,
		func(implementation any) any {
			server, ok := implementation.(minttypes.MsgServer)
			if !ok {
				panic(fmt.Errorf("unexpected mint MsgServer implementation %T", implementation))
			}
			return &guardedMintMsgServer{MsgServer: server}
		},
		appModule.AppModule.RegisterServices,
	)
}

func validateGovParameterPolicy(params govv1.Params) error {
	if err := validateOnlyNativeCoins(params.MinDeposit); err != nil {
		return fmt.Errorf("governance minimum deposit: %w", err)
	}
	if err := validateOnlyNativeCoins(params.ExpeditedMinDeposit); err != nil {
		return fmt.Errorf("governance expedited minimum deposit: %w", err)
	}
	return nil
}

type guardedGovMsgServer struct {
	govv1.MsgServer
}

func (server *guardedGovMsgServer) UpdateParams(
	ctx context.Context,
	req *govv1.MsgUpdateParams,
) (*govv1.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, nilParameterUpdate("gov")
	}
	if err := validateGovParameterPolicy(req.Params); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	if err := req.Params.ValidateBasic(); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	return server.MsgServer.UpdateParams(ctx, req)
}

type guardedGovAppModule struct {
	gov.AppModule
}

func newGuardedGovAppModule(upstream gov.AppModule) guardedGovAppModule {
	return guardedGovAppModule{AppModule: upstream}
}

func (appModule guardedGovAppModule) RegisterServices(cfg module.Configurator) {
	registerServicesWithMsgServerGuard(
		cfg,
		govv1.Msg_serviceDesc.ServiceName,
		func(implementation any) any {
			server, ok := implementation.(govv1.MsgServer)
			if !ok {
				panic(fmt.Errorf("unexpected governance MsgServer implementation %T", implementation))
			}
			return &guardedGovMsgServer{MsgServer: server}
		},
		appModule.AppModule.RegisterServices,
	)
}

func validateFeeMarketParameterPolicy(params feemarkettypes.Params) error {
	minimumGasPrice := sdkmath.LegacyOneDec()
	if params.BaseFee.IsNil() || params.BaseFee.LT(minimumGasPrice) {
		return fmt.Errorf("fee market base fee must be at least 1 agxn per gas")
	}
	if params.BaseFee.GT(maximumFeeMarketGasPrice) {
		return fmt.Errorf("fee market base fee cannot exceed 1 gxn per gas")
	}
	if params.MinGasPrice.IsNil() || params.MinGasPrice.LT(minimumGasPrice) {
		return fmt.Errorf("fee market minimum gas price must be at least 1 agxn per gas")
	}
	if params.MinGasPrice.GT(maximumFeeMarketGasPrice) {
		return fmt.Errorf("fee market minimum gas price cannot exceed 1 gxn per gas")
	}
	if params.NoBaseFee {
		return fmt.Errorf("fee market base fee must remain enabled")
	}
	if params.EnableHeight != 0 {
		return fmt.Errorf("fee market enable height must remain 0, got %d", params.EnableHeight)
	}
	if params.BaseFeeChangeDenominator <= 1 {
		return fmt.Errorf("fee market base fee change denominator must be greater than 1")
	}
	if params.ElasticityMultiplier != feemarkettypes.DefaultParams().ElasticityMultiplier {
		return fmt.Errorf(
			"fee market elasticity multiplier must remain %d, got %d",
			feemarkettypes.DefaultParams().ElasticityMultiplier,
			params.ElasticityMultiplier,
		)
	}
	return nil
}

type guardedFeeMarketMsgServer struct {
	feemarkettypes.MsgServer
}

func (server *guardedFeeMarketMsgServer) UpdateParams(
	ctx context.Context,
	req *feemarkettypes.MsgUpdateParams,
) (*feemarkettypes.MsgUpdateParamsResponse, error) {
	if req == nil {
		return nil, nilParameterUpdate(feemarkettypes.ModuleName)
	}
	if req.Params.BaseFee.IsNil() {
		return nil, invalidParameterPolicy(fmt.Errorf("fee market base fee cannot be nil"))
	}
	if err := req.Params.Validate(); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	if err := validateFeeMarketParameterPolicy(req.Params); err != nil {
		return nil, invalidParameterPolicy(err)
	}
	return server.MsgServer.UpdateParams(ctx, req)
}

type guardedFeeMarketAppModule struct {
	feemarket.AppModule
	keeper feemarketkeeper.Keeper
}

func newGuardedFeeMarketAppModule(
	upstream feemarket.AppModule,
	keeper feemarketkeeper.Keeper,
) guardedFeeMarketAppModule {
	return guardedFeeMarketAppModule{
		AppModule: upstream,
		keeper:    keeper,
	}
}

func (appModule guardedFeeMarketAppModule) RegisterServices(cfg module.Configurator) {
	registerServicesWithMsgServerGuard(
		cfg,
		feeMarketMsgServiceName,
		func(implementation any) any {
			server, ok := implementation.(feemarkettypes.MsgServer)
			if !ok {
				panic(fmt.Errorf("unexpected fee market MsgServer implementation %T", implementation))
			}
			return &guardedFeeMarketMsgServer{MsgServer: server}
		},
		appModule.AppModule.RegisterServices,
	)
}

// BeginBlock keeps the upstream EIP-1559 calculation but clamps its persisted
// result to the arithmetic-safe policy interval. With elasticity fixed at two,
// this bounds the largest intermediate LegacyDec multiplication below 256 bits.
func (appModule guardedFeeMarketAppModule) BeginBlock(goCtx context.Context) (err error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("calculate fee market base fee: %v", recovered)
		}
	}()

	baseFee := appModule.keeper.CalculateBaseFee(ctx)
	if baseFee.IsNil() {
		return nil
	}
	baseFee = sdkmath.LegacyMaxDec(baseFee, sdkmath.LegacyOneDec())
	baseFee = sdkmath.LegacyMinDec(baseFee, maximumFeeMarketGasPrice)
	appModule.keeper.SetBaseFee(ctx, baseFee)
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		feemarkettypes.EventTypeFeeMarket,
		sdk.NewAttribute(feemarkettypes.AttributeKeyBaseFee, baseFee.String()),
	))
	return nil
}
