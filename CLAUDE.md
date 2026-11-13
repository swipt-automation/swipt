# swipt

## Introduction

@README.md

## Commands

`bun dev` prints its commands with their arguments; `swipt [COMMAND...] --help` does the same for the CLI.

- `bun dev install [-d NAME|UDID]` - Build Swipt, install it on the device, and set it up like `swipt sideload install`
- `bun dev frameworks` - Install the React Native frameworks
- `bun dev strings` - Sync cli/locales with the strings of the CLI
- `bun run --cwd cli build` - Build the CLI
- `bun typecheck` - Type-check cli and dev
- `make lib` - Build the SDK runtime and libswipt
- `make drivers` - Build swipt-driver for every release target
- `make build_ios` / `make build_macos` - Build the Debug app
- `make runtime_bundle` - Bundle the Swiplet UI runtime
- `make build_ios_ipa` - Build Swipt.ipa
- `make proto` - Regenerate the gRPC code
- `make fmt` - Format the sources
- `make lint` - Lint Go
