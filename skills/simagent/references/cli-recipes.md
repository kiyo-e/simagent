# simagent CLI Recipes

Use these command templates directly. Prefer `--json` for machine-readable output.

## Device Selection

```bash
./simagent target list --json
./simagent target set booted --json
./simagent target show --json
```

## Frame Capture

```bash
./simagent frame --json
./simagent frame --out /tmp/simagent-run --format jpg --json
./simagent frame --interactive-only=false --order stable --min-area 100 --json
./simagent frame --include-roles button,textfield --exclude-roles cell --json
```

`frame` writes artifacts (`screen.*`, `ui.raw.json`, `elements.json`, `transform.json`, `annotated.png`) and refreshes `~/.config/simagent/last_frame.json`.

## UI Actions

Tap:

```bash
./simagent ui tap --index 1 --json
./simagent ui tap --id login_button --json
./simagent ui tap 120 340 --unit pt --json
./simagent ui tap 480 1360 --unit px --from /tmp/simagent-run/elements.json --json
```

Type:

```bash
./simagent ui type "hello@example.com" --into --index 2 --json
./simagent ui type "123456" --into --id otp_field --json
./simagent ui type "plain text input" --json
```

Swipe and device button:

```bash
./simagent ui swipe up --index 4 --distance 260 --json
./simagent ui swipe left --json
./simagent ui button HOME --json
```

## App Actions

```bash
./simagent app openurl "myapp://debug" --json
./simagent app launch --bundle-id com.example.app --args --feature-flag on --json
./simagent app terminate --bundle-id com.example.app --json
./simagent app list --json
```

## Raw Passthrough

```bash
./simagent raw simctl getenv booted SIMULATOR_RUNTIME_VERSION --json
./simagent raw idb list-targets --json
```

Use `raw` only when no first-class command already exists.
