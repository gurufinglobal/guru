# Contributing to Guru

Thank you for contributing to Guru. Please follow this guide so that changes
can be reviewed and validated consistently.

All contributors must follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Before You Start

Open a [GitHub issue](https://github.com/gurufinglobal/guru/issues) before
starting a change that affects public interfaces, network behavior, consensus,
configuration compatibility, or repository architecture. Small fixes and
documentation improvements may be submitted directly as a pull request.

Never include private keys, seed phrases, credentials, production configuration,
or other sensitive information in issues, commits, logs, or pull requests.

## Target Branch

For the current v2.1 development cycle:

- Submit normal development changes to `dev-v2.1`.
- Use `release/**` branches only for release stabilization coordinated by the
  maintainers.
- Submit changes directly to `main` only when maintainers explicitly request it.

Check the pull request base branch before submitting, because the active
development branch may change in a future release cycle.

## Development Requirements

Guru requires the Go version declared in `go.mod`. The repository contains two
Go modules: the root application module and the `oracle` module. Changes must
keep both modules consistent.

Install or download dependencies through the Go module files. Do not commit
local build output, downloaded tools, credentials, or editor-specific files.

## Making Changes

- Keep each pull request focused on one logical change.
- Add or update tests when behavior changes or a defect is fixed.
- Update `README.md` and relevant module documentation when public interfaces,
  environment variables, configuration, ports, commands, or operational
  behavior change.
- When protobuf definitions change, regenerate and format the generated files
  with `make proto-all` and verify them with `make proto-check`.
- Do not manually change project versions for normal pull requests. Release
  versions are derived from `v*` Git tags and published through the release
  workflow. Update versioned examples only when a release-specific change
  requires it.

The project follows [Semantic Versioning](https://semver.org/) for releases.

## Validation

Run the checks relevant to your change before submitting a pull request. The
standard validation set is:

```bash
make mod-check
make format-check
make lint
make test-cover
make version-smoke
```

Additional checks apply to specific changes:

```bash
# Protobuf definitions or generated protobuf files
make proto-check

# GoReleaser or release packaging configuration
make release-check
```

If a required check cannot be run locally, explain why in the pull request.
All required GitHub Actions checks must pass before merge.

## Pull Request

Include the following information in the pull request description:

- the problem and the intended behavior;
- the scope of the change and any important design decisions;
- tests and commands used for validation;
- compatibility, configuration, migration, or operational impact; and
- related issues, if any.

Respond to review comments with follow-up commits or a clear explanation. Avoid
unrelated formatting or refactoring so that the change remains easy to review.
