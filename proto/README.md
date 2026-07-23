# Guru Protobuf Guide

Guru uses two generated Go boundaries from the same protobuf sources:

- internal gogo runtime types under `x/<module-path>/types` for node, consensus,
  state, transactions, and registered services;
- external Pulsar API types under `api/guru/<module>/v1` for protov2 clients.

Do not mix the external Pulsar packages into production node code. Do not add
legacy Amino transactions, `sdk.StdTx`, or compatibility decoder fallbacks.

## Package And Generation

For a new module at `<module-path>`:

- Place proto sources under `proto/guru/<module>/v1`.
- Use the protobuf package `guru.<module>.v1`.
- Set `go_package` to
  `github.com/gurufinglobal/guru/v3/x/<module-path>/types`.
- Run `make proto-format`, `make proto-lint`, and `make proto-gen`.
- Never hand-edit generated files under `x/<module>/types` or `api/`.

`make proto-gen` runs the pinned proto-builder Docker image. The internal gogo
generator stages packages by their `go_package` and automatically copies every
generated `x/<module-path>/types` package into the repository. The external Pulsar template
uses Buf managed mode to override the Go package prefix to
`github.com/gurufinglobal/guru/v3/api`, so the source schema remains the single
package authority for both outputs. Pulsar output is also staged before
`api/guru` is replaced, so files left by a removed or renamed proto are pruned
only after both generators succeed. Internal generated files for a removed
module are pruned as well, while handwritten files in its `types` package are
left untouched.

Adding another `proto/guru/<module>/v1` package does not require adding the
module name to the generation script or Makefile.

## Runtime Boundary

Production packages under `app`, `cmd`, `oracle`, and `x` use the internal gogo
types. External Pulsar types must remain isolated to public API compatibility
tests and external client code.

Generated internal files necessarily import `github.com/cosmos/gogoproto`.
Handwritten Guru source should not import it except at an explicit, tested SDK
boundary such as the hybrid protobuf resolver, descriptor compatibility, or
SDK JSON/genesis integration.

State values should use the internal generated value types with the injected
application codec, for example `codec.CollValue[types.Params](cdc)`. Do not use
external Pulsar pointers as keeper state values.

## Msg Requirements

For every `tx.proto`:

- Add `option (cosmos.msg.v1.service) = true` to the `Msg` service.
- Add `option (cosmos.msg.v1.signer) = "<field_name>"` to every request message.
- Use `[(cosmos_proto.scalar) = "cosmos.AddressString"]` for signer addresses.
- Register request implementations as `sdk.Msg` in
  `x/<module>/types/codec.go`.
- Register responses as `cosmos.tx.v1beta1.MsgResponse`.
- Register the generated Msg service descriptor with
  `msgservice.RegisterMsgServiceDesc`.
- Wire the module so `BasicManager.RegisterInterfaces` and
  `ModuleManager.RegisterServices` cover it.

## Transaction And Amino Policy

The app uses the Cosmos SDK standard TxConfig and decoder with only
`SIGN_MODE_DIRECT` and `SIGN_MODE_DIRECT_AUX`. There is no Guru-specific decode
fallback.

Generic Amino transaction encode/decode and legacy Amino signing remain
unsupported. The narrow legacy codec initialized by `gurud` is only for Cosmos
SDK and Cosmos EVM crypto-key armor; it is not exposed through the app or client
transaction configuration.

## Tests For New Modules

When adding a new transaction module:

- Build and decode a protobuf transaction containing each new Msg, including a
  representative nested Guru message when present.
- Verify `GetMsgs`, `GetMsgsV2`, signer extraction, `WrapTxBuilder`, and
  deterministic re-encoding.
- Verify internal gogo and external Pulsar wire, descriptor, gRPC, and REST JSON
  compatibility for public surfaces.
- Add keeper, genesis/export, CLI, and local-node coverage appropriate to the
  module's consensus and operator impact.

Keep module-local validation in the module. Add app-level genesis validation
only for cross-module or chain-wide invariants.
