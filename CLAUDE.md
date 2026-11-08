# swipt

## Introduction

@README.md

## Commands

`bun dev` prints its commands with their arguments; `swipt [COMMAND...] --help` does the same for the CLI.

- `bun dev install [-d NAME|UDID]` - Build Swipt, install it on the device, check the installed build, and set it up like `swipt sideload install`
- `bun dev frameworks` - Build ExpoModulesCore and install the React Native frameworks
- `bun dev strings` - Check cli/locales against the strings of the CLI and remove the stale ones
- `bun run --cwd cli build` - Build the CLI from cli/src into cli/dist with tsdown
- `bun typecheck` - Build the CLI, then type-check cli and dev
- `make lib` - Build the SDK runtime and libswipt
- `make build_ios` / `make build_macos` - Build the Debug app into build/DerivedData
- `make runtime_bundle` - Bundle the Swiplet UI runtime
- `make build_ios_ipa` - Build Swipt.ipa
- `make proto` - Regenerate the gRPC code
- `make fmt` - Format the sources
- `make lint` - Lint Go
