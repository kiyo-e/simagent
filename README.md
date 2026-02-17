# simagent

`simagent` is a macOS CLI for iOS Simulator automation that combines:

- `xcrun simctl` for screenshots and app-level actions
- `idb` for accessibility tree inspection and UI interactions

It is designed for fast `frame -> action -> frame` loops during live debugging sessions (including Flutter apps running via `flutter run`).

## Features

- Unified target selection (`booted` or explicit UDID)
- `frame` command that generates:
  - raw screenshot
  - raw UI tree
  - normalized interactive elements with stable indexes
  - transform metadata (`pt <-> px`)
  - annotated screenshot with index overlays
- UI actions by coordinates, index, or element ID:
  - tap / type / swipe / button
- App helper actions:
  - openurl / launch / terminate / list
- Machine-readable JSON output (`--json`)
- Last-frame state reuse for follow-up UI actions

## Requirements

- macOS
- Xcode + Command Line Tools (`xcrun`, `simctl`)
- iOS Simulator (booted device)
- `idb` installed and usable for your simulator target
- Go 1.22+ (to build from source)

## Install (from source)

```bash
git clone <your-repo-url>
cd simagent
go build -o simagent .
```

## Build and Distribute with GitHub Actions

Workflow file: `.github/workflows/build-and-release.yml`

- On pull requests and pushes to `main`, it runs `go test ./...` and builds:
  - `simagent_darwin_arm64.tar.gz`
  - `simagent_darwin_amd64.tar.gz`
- On tags matching `v*` (example: `v0.1.0`), it publishes a GitHub Release with:
  - both macOS archives
  - `checksums.txt`

Release trigger example:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Quick Start

1. Capture frame:

```bash
./simagent frame --target booted --json
```

2. Tap by index from the latest frame:

```bash
./simagent ui tap --index 1 --json
```

3. Type into a target field:

```bash
./simagent ui type "hello@example.com" --into --index 2 --json
```

4. Re-capture frame:

```bash
./simagent frame --json
```

## Command Overview

Global options:

- `--target booted|<UDID>`
- `--timeout <duration>`
- `--json`
- `--quiet`

Top-level commands:

- `target` (`list`, `set`, `show`)
- `frame`
- `ui` (`tap`, `type`, `swipe`, `button`)
- `app` (`openurl`, `launch`, `terminate`, `list`)
- `raw` (`simctl`, `idb`)

Run without args to see usage:

```bash
./simagent
```

## Artifacts from `frame`

By default, `frame` writes outputs under:

- `/tmp/simagent/<timestamp>/` (resolved from macOS temp dir)

Generated files:

- `screen.png` (or `screen.jpg`)
- `ui.raw.json`
- `elements.json`
- `transform.json`
- `annotated.png`

## JSON Error Shape

When `--json` is set, failures are returned as:

```json
{
  "ok": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "human readable message",
    "details": {}
  }
}
```

## Development

```bash
gofmt -w main.go
go test ./...
go build -o simagent .
```

## License

MIT. See `LICENSE`.
