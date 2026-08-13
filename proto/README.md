# Guru Protobuf workflow

The schemas under `proto/guru` generate the internal gogo types used by the
Cosmos SDK v0.53 application. Generated files are written to each module's
`x/<module>/types` package according to its `go_package` option.

Use the pinned proto-builder image through these targets:

```sh
make proto-format
make proto-lint
make proto-gen
```

Do not edit `*.pb.go` or `*.pb.gw.go` files manually. The standalone
`oracle` module imports the root `x/oracle/types` package through its local
module replacement and therefore does not maintain a second schema copy.
