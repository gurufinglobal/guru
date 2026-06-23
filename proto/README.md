# Guru Protobuf Guide

Guru proto sources are generated with Pulsar and should not add legacy Amino or
`sdk.StdTx` compatibility paths. The app still runs on Cosmos SDK v0.54, whose
transaction and codec boundary is hybrid. New Guru modules must follow the rules
below so SDK transactions and Guru custom transactions keep decoding safely.

## No Direct GoGo Proto In New Guru Modules

New Guru modules must not directly import `github.com/cosmos/gogoproto`.
Imported SDK, CometBFT, IBC, or Cosmos EVM packages may still use gogo
internally because that is part of the current dependency stack. Keep that
compatibility at dependency boundaries only; do not add new Guru codegen,
helpers, message types, or module logic that depends directly on gogo APIs.

The repo has a policy test that blocks direct gogo imports in Guru source except
for narrow SDK-boundary exceptions.

## Package And Generation

- Place Guru module proto files under `proto/guru/<module>/v1`.
- Use package names under `guru.<module>.v1`.
- Set `go_package` to `github.com/gurufinglobal/guru/v3/api/guru/<module>/v1;<module>v1`.
- Regenerate generated files through the repo proto workflow. Do not hand-edit
  files under `api/`.
- The generated `.pulsar.go` files must be linked into the app binary so their
  descriptors are registered in `protoregistry.GlobalFiles`.

## Msg Requirements

For any `tx.proto`:

- Add `option (cosmos.msg.v1.service) = true` to the `Msg` service.
- Add `option (cosmos.msg.v1.signer) = "<field_name>"` to every request message.
- Use `[(cosmos_proto.scalar) = "cosmos.AddressString"]` for signer address
  fields.
- Register every Msg implementation as `sdk.Msg` in `x/<module>/types/codec.go`.
- Register every Msg response as `cosmos.tx.v1beta1.MsgResponse`.
- Wire the module into the app so `BasicManager.RegisterInterfaces(...)` and
  `ModuleManager.RegisterServices(...)` cover the module.

## Tx Decode Compatibility

The app uses the SDK default tx decoder for normal SDK/gogo transactions. It
only falls back to a Guru Pulsar-compatible decode path when the SDK decoder
cannot resolve a nested `guru.*` message in `TxBody`.

This means:

- SDK module transactions should keep using the default SDK path.
- Guru Pulsar Msgs with nested Guru messages, such as `OracleTask` or
  `SeparationRatio`, are supported.
- Future `guru.*` Msgs are supported when their generated descriptors and
  interface registrations are present.
- Missing interface registration will still fail when unpacking the Msg from
  `Any`; this is intentional.
- Missing or invalid signer annotations will still fail signer extraction or
  signature verification; this is intentional.

## Tests To Add With New Modules

When adding a new Guru tx module, add focused tests that:

- Build a protobuf tx with at least one nested Guru message field.
- Decode it through `appparams.MakeEncodingConfig(...).TxConfig.TxDecoder()`.
- Assert `GetMsgs`, `GetMsgsV2`, signer extraction, `WrapTxBuilder`, and
  re-encoding all succeed.
- Add a CLI or e2e smoke when the Msg is user-facing.

Keep module-local validation in the module. Add app-level genesis validation
only for invariants that span modules or chain policy.
