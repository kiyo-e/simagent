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
  - normalized elements with stable indexes (`label/value/visible/offscreen/nearbyLabel` included)
  - transform metadata (`pt <-> px`)
  - annotated screenshot with index overlays
  - optional stability sampling via `--stable`
- UI actions by coordinates, index, element ID, or text:
  - tap / type / clear / swipe / wait / button / flow run
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
./simagent ui type --text "hello@example.com" --into --index 2 --replace --verify --json
```

4. Wait for a UI transition:

```bash
./simagent ui wait --has-text "確認" --interactive-min 1 --timeout 15s --json
```

5. Re-capture frame:

```bash
./simagent frame --stable --json
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
- `ui` (`tap`, `type`, `clear`, `swipe`, `wait`, `button`, `flow run`)
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

`elements.json` includes stable identifiers and automation hints:

- `index`, `id`, `role`, `label`, `value`
- `enabled`, `visible`, `offscreen`
- `nearbyLabel`, `frame`, `center`, `source`

## UI Command Notes

`ui type` now supports `--text` as the primary input. Positional text is still accepted for compatibility.
When `--into`/focused fields are available, simagent retries partial `idb ui text` inputs and appends missing suffixes automatically.
For fragile numeric fields (phone, zip, OTP), prefer `--into` with selector + `--replace --ascii`.

```bash
./simagent ui type --text "170" --into --label "身長" --replace --ascii --verify --json
./simagent ui type --text "090-0000-0000" --into --label "電話番号" --replace --ascii --json
```

`ui clear` clears a focused input field by selector:

```bash
./simagent ui clear --label "郵便番号" --json
```

`ui tap` supports text-based selectors and falls back to system-UI heuristics for common top-bar actions (for example add/cancel style buttons):

```bash
./simagent ui tap --label "次へ" --json
./simagent ui tap --contains "スキップ" --json
```

`ui wait` polls `idb ui describe-all --json` until a condition is satisfied:

```bash
./simagent ui wait --has-text "送信完了" --interactive-min 1 --timeout 20s --interval 700ms --json
```

`ui flow run` executes JSON-defined `tap/type/swipe/wait` steps:

```bash
./simagent ui flow run --file ./fixtures/flows/signup-minimal.json --json
```

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
